package roomkeeper

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/day20/internal/util/id"
	"github.com/alex65536/day20/internal/util/slogx"
)

type roomExt struct {
	room     *room
	mu       sync.Mutex
	locked   bool
	lastSeen time.Time
}

func newRoomExt(data RoomFullData) *roomExt {
	r := &roomExt{
		room:     newRoom(data),
		locked:   false,
		lastSeen: time.Now(),
	}
	return r
}

func (r *roomExt) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSeen = time.Now()
	r.locked = false
}

type Keeper struct {
	db    DB
	sched Scheduler
	opts  Options
	log   *slog.Logger

	gctx   context.Context
	cancel func()
	wg     sync.WaitGroup

	mu    sync.RWMutex
	rooms map[string]*roomExt
}

var _ roomapi.API = (*Keeper)(nil)

func New(
	ctx context.Context,
	log *slog.Logger,
	db DB,
	sched Scheduler,
	opts Options,
) (*Keeper, error) {
	opts.FillDefaults()
	rooms, err := db.ListActiveRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active rooms: %w", err)
	}
	gctx, cancel := context.WithCancel(context.Background())
	k := &Keeper{
		db:     db,
		sched:  sched,
		opts:   opts,
		log:    log,
		gctx:   gctx,
		cancel: cancel,
		rooms:  make(map[string]*roomExt, len(rooms)),
	}
	for _, desc := range rooms {
		k.rooms[desc.Info.ID] = newRoomExt(desc)
	}
	k.wg.Add(1)
	go k.gc()
	return k, nil
}

func (k *Keeper) gc() {
	defer k.wg.Done()
	ticker := time.NewTicker(k.opts.GCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			var roomsToStop []*roomExt
			now := time.Now()
			func() {
				k.mu.Lock()
				defer k.mu.Unlock()
				for roomID, r := range k.rooms {
					if mustDel := func() bool {
						r.mu.Lock()
						defer r.mu.Unlock()
						if r.locked {
							return false
						}
						if now.Sub(r.lastSeen) <= k.opts.RoomLivenessTimeout {
							return false
						}
						r.locked = true
						return true
					}(); mustDel {
						roomsToStop = append(roomsToStop, r)
						delete(k.rooms, roomID)
					}
				}
			}()
			for _, room := range roomsToStop {
				k.stop(k.gctx, room)
			}
		case <-k.gctx.Done():
			return
		}
	}
}

func (k *Keeper) abortRoomJob(r *roomExt, reason string) {
	curJob := r.room.Job()
	if curJob == nil {
		return
	}
	game, err := r.room.GameExt()
	if err != nil {
		if !errors.Is(err, ErrGameNotReady) {
			k.log.Warn("cannot extract game from aborted job",
				slog.String("room_id", r.room.ID()),
				slog.String("job_id", curJob.Desc.ID),
			)
		}
		game = nil
	}
	k.sched.OnJobFinished(curJob.Desc.ID, NewStatusAborted(reason), game)
	r.room.SetJob(nil)
}

func (k *Keeper) stop(ctx context.Context, r *roomExt) {
	r.mu.Lock()
	locked := r.locked
	r.mu.Unlock()
	if !locked {
		panic("must not happen")
	}
	log := k.logFromCtx(ctx)
	roomID := r.room.ID()
	k.abortRoomJob(r, "room stopped")
	r.room.Stop(log)
	if err := k.db.DeleteRoom(ctx, roomID); err != nil {
		log.Error("cannot delete room from db", slog.String("room_id", roomID), slogx.Err(err))
	}
}

func (k *Keeper) Close() {
	select {
	case <-k.gctx.Done():
	default:
		k.cancel()
		k.wg.Wait()
	}
}

func (k *Keeper) logFromCtx(ctx context.Context) *slog.Logger {
	rid := httputil.ExtractReqID(ctx)
	log := k.log
	if rid != "" {
		log = log.With(slog.String("rid", rid))
	}
	return log
}

func (k *Keeper) getAndAcquireRoom(roomID string) (*roomExt, error) {
	r, err := k.doGetRoom(roomID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locked {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrLocked,
			Message: "some other request already uses the room",
		}
	}
	r.locked = true
	return r, nil
}

func (k *Keeper) Update(ctx context.Context, req *roomapi.UpdateRequest) (*roomapi.UpdateResponse, error) {
	log := k.logFromCtx(ctx).With(slog.String("room_id", req.RoomID))

	if req.Delta != nil {
		req.Delta.FixTimestamps(delta.TimestampDiff{
			TheirNow: req.Timestamp,
			OurNow:   delta.NowTimestamp(),
		})
		// Do not re-assign req.Timestamp = delta.NowTimestamp() to simplify double fix detection.
	}

	room, err := k.getAndAcquireRoom(req.RoomID)
	if err != nil {
		return nil, err
	}
	defer room.Release()

	log.Info("updating room")

	job := room.room.Job()
	if job == nil {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job currently running, nothing to update",
		}
	}

	if !k.sched.IsContestRunning(job.ContestID) {
		k.abortRoomJob(room, "contest canceled")
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "job has just been canceled",
		}
	}

	status, game, updErr := func() (JobStatus, *battle.GameExt, error) {
		status, state, updErr := room.room.Update(log, req)
		var game *battle.GameExt
		if status.Kind.IsFinished() && state != nil && state.Info != nil {
			var err error
			game, err = state.GameExt()
			if err != nil {
				game = nil
				log.Warn("cannot create resulting game", slogx.Err(err))
				if status.Kind != JobAborted {
					status = NewStatusAborted("job cannot be collected into game")
				}
				if updErr == nil {
					updErr = &roomapi.Error{
						Code:    roomapi.ErrBadRequest,
						Message: "result cannot be collected into game",
					}
				}
			}
		}
		return status, game, updErr
	}()

	if status.Kind.IsFinished() {
		k.sched.OnJobFinished(job.Desc.ID, status, game)
	}

	if err := k.db.UpdateRoom(ctx, room.room.ID(), room.room.Data()); err != nil {
		log.Error("cannot update room in db", slogx.Err(err))
		return nil, fmt.Errorf("update room in db: %w", err)
	}

	if updErr != nil {
		log.Info("error updating room", slogx.Err(err))
		return nil, fmt.Errorf("cannot update: %w", updErr)
	}

	return &roomapi.UpdateResponse{}, nil
}

func (k *Keeper) Job(ctx context.Context, req *roomapi.JobRequest) (*roomapi.JobResponse, error) {
	log := k.logFromCtx(ctx).With(slog.String("room_id", req.RoomID))

	timeout := req.Timeout
	if timeout <= 0 {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrBadRequest,
			Message: "non-positive timeout",
		}
	}
	timeout = min(timeout, k.opts.MaxJobFetchTimeout)

	room, err := k.getAndAcquireRoom(req.RoomID)
	if err != nil {
		return nil, err
	}
	defer room.Release()

	log.Info("fetching job for room")

	subctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	job, err := k.sched.NextJob(subctx)
	if err != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		select {
		case <-subctx.Done():
			return nil, &roomapi.Error{
				Code:    roomapi.ErrNoJob,
				Message: "no job to run",
			}
		default:
		}
		log.Warn("error polling for job", slogx.Err(err))
		return nil, fmt.Errorf("poll for job: %w", err)
	}

	k.abortRoomJob(room, "job lost by room")
	room.room.SetJob(job)

	if err := k.db.UpdateRoom(ctx, room.room.ID(), room.room.Data()); err != nil {
		log.Error("cannot update room in db", slogx.Err(err))
		return nil, fmt.Errorf("update room in db: %w", err)
	}

	return &roomapi.JobResponse{
		Job: job.Desc.Clone(),
	}, nil
}

func (k *Keeper) Hello(ctx context.Context, req *roomapi.HelloRequest) (*roomapi.HelloResponse, error) {
	log := k.logFromCtx(ctx)

	if !slices.Contains(req.SupportedProtoVersions, roomapi.ProtoVersion) {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrIncompatibleProto,
			Message: "no supported proto versions",
		}
	}

	var (
		roomID string
		data   RoomFullData
	)
	func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		roomID = id.ID()
		if _, ok := k.rooms[roomID]; ok {
			panic("id collision")
		}
		data = RoomFullData{
			Info: RoomInfo{
				ID:   roomID,
				Name: roomID, // TODO: generate nice room names!
			},
			Data: RoomData{
				Job: nil,
			},
		}
		k.rooms[roomID] = newRoomExt(data)
	}()

	log = log.With(slog.String("room_id", roomID))
	log.Info("created room")

	if err := k.db.CreateRoom(ctx, data.Info); err != nil {
		log.Error("cannot create room in db", slogx.Err(err))
		return nil, fmt.Errorf("create room in db: %w", err)
	}

	return &roomapi.HelloResponse{
		RoomID:       roomID,
		ProtoVersion: roomapi.ProtoVersion,
	}, nil
}

func (k *Keeper) Bye(ctx context.Context, req *roomapi.ByeRequest) (*roomapi.ByeResponse, error) {
	log := k.logFromCtx(ctx).With("room_id", req.RoomID)

	room, err := k.getAndAcquireRoom(req.RoomID)
	if err != nil {
		return nil, err
	}
	// No release needed, we are going to delete the room!

	log.Info("deleting room")
	k.mu.Lock()
	delete(k.rooms, room.room.ID())
	k.mu.Unlock()

	k.stop(ctx, room)

	return &roomapi.ByeResponse{}, nil
}

func (k *Keeper) ListRooms() []RoomInfo {
	k.mu.RLock()
	defer k.mu.RUnlock()
	res := make([]RoomInfo, 0, len(k.rooms))
	for _, room := range k.rooms {
		res = append(res, room.room.Info())
	}
	slices.SortFunc(res, func(a, b RoomInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return res
}

func (k *Keeper) doGetRoom(roomID string) (*roomExt, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	room, ok := k.rooms[roomID]
	if !ok {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoSuchRoom,
			Message: "no such room",
		}
	}
	return room, nil
}

func (k *Keeper) RoomGameExt(roomID string) (*battle.GameExt, error) {
	room, err := k.doGetRoom(roomID)
	if err != nil {
		return nil, err
	}
	g, err := room.room.GameExt()
	if err != nil {
		return nil, fmt.Errorf("room game: %w", err)
	}
	return g, nil
}

func (k *Keeper) RoomInfo(roomID string) (RoomInfo, error) {
	room, err := k.doGetRoom(roomID)
	if err != nil {
		return RoomInfo{}, err
	}
	return room.room.Info(), nil
}

func (k *Keeper) Subscribe(roomID string) (ch <-chan struct{}, cancel func(), ok bool) {
	room, err := k.doGetRoom(roomID)
	if err != nil {
		return nil, nil, false
	}
	ch, cancel = room.room.Subscribe()
	return ch, cancel, true
}

func (k *Keeper) RoomStateDelta(roomID string, old delta.RoomCursor) (*delta.RoomState, delta.RoomCursor, error) {
	room, err := k.doGetRoom(roomID)
	if err != nil {
		return nil, delta.RoomCursor{}, err
	}
	d, cursor, err := room.room.StateDelta(old)
	if err != nil {
		return nil, delta.RoomCursor{}, fmt.Errorf("room state: %w", err)
	}
	return d, cursor, nil
}