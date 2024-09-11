package roomkeeper

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/day20/internal/util/slogx"
)

type roomExt struct {
	room     *room
	mu       sync.Mutex
	locked   bool
	lastSeen time.Time
}

func newRoomExt(desc RoomDesc) *roomExt {
	r := &roomExt{
		room:     newRoom(desc),
		locked:   false,
		lastSeen: time.Now(),
	}
	return r
}

func (r *roomExt) release() {
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
	log *slog.Logger,
	db DB,
	sched Scheduler,
	opts Options,
	rooms []RoomDesc,
) *Keeper {
	opts.FillDefaults()
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
		k.rooms[desc.RoomID] = newRoomExt(desc)
	}
	k.wg.Add(1)
	go k.gc()
	return k
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

func (k *Keeper) stop(ctx context.Context, r *roomExt) {
	log := k.logFromCtx(ctx)
	roomID := r.room.ID()
	if curJob := r.room.Job(); curJob != nil {
		k.sched.OnJobFinished(curJob.JobID, JobAborted)
	}
	r.room.Stop()
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
	k.mu.Lock()
	defer k.mu.Unlock()
	r, ok := k.rooms[roomID]
	if !ok {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoSuchRoom,
			Message: "room not found",
		}
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

	room, err := k.getAndAcquireRoom(req.RoomID)
	if err != nil {
		return nil, err
	}
	defer room.release()

	log.Info("updating room", slog.String("room_id", req.RoomID))

	job := room.room.Job()
	if job == nil {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job currently running, nothing to update",
		}
	}
	if req.Done && req.Error != "" {
		log.Warn("received error update from client", slog.String("err", req.Error))
	}
	status, state, updErr := func() (JobStatus, *delta.State, error) {
		if !k.sched.IsContestRunning(job.ContestID) {
			room.room.SetJob(nil)
			return JobAborted, nil, &roomapi.Error{
				Code:    roomapi.ErrNoJobRunning,
				Message: "job has just been canceled",
			}
		}
		return room.room.Update(req)
	}()

	mustAbort := false
	defer func() {
		if status == JobRunning {
			return
		}
		if mustAbort {
			status = JobAborted
		}
		k.sched.OnJobFinished(job.JobID, status)
	}()

	if err := k.db.UpdateRoom(ctx, room.room.Desc().Clone()); err != nil {
		log.Error("cannot update room in db", slogx.Err(err))
		mustAbort = true
		return nil, fmt.Errorf("update room in db: %w", err)
	}

	if status == JobSucceeded {
		if updErr != nil {
			panic(fmt.Sprintf("must not happen: %v", err))
		}
		game, err := state.GameExt()
		if err != nil {
			mustAbort = true
			log.Warn("cannot create resulting game", slogx.Err(err))
			return nil, &roomapi.Error{
				Code:    roomapi.ErrBadRequest,
				Message: "result cannot be collected into game",
			}
		}
		if err := k.db.AddGame(ctx, job.ContestID, game); err != nil {
			mustAbort = true
			log.Error("cannot add game into db", slogx.Err(err))
			return nil, fmt.Errorf("add game in db: %w", err)
		}
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
	defer room.release()

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

	if curJob := room.room.Job(); curJob != nil {
		k.sched.OnJobFinished(curJob.JobID, JobAborted)
	}
	room.room.SetJob(job)

	if err := k.db.UpdateRoom(ctx, room.room.Desc().Clone()); err != nil {
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
		desc   RoomDesc
	)
	func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		roomID = genUnusedKey(k.rooms)
		desc = RoomDesc{
			RoomID: roomID,
			Job:    nil,
		}
		k.rooms[roomID] = newRoomExt(desc)
	}()

	log.Info("created room", slog.String("room_id", roomID))

	if err := k.db.UpdateRoom(ctx, desc); err != nil {
		log.Error("cannot update room in db", slogx.Err(err))
		return nil, fmt.Errorf("update room in db: %w", err)
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

	log.Info("deleting room", slog.String("room_id", req.RoomID))
	k.mu.Lock()
	delete(k.rooms, room.room.ID())
	k.mu.Unlock()

	k.stop(ctx, room)

	return &roomapi.ByeResponse{}, nil
}

func (k *Keeper) ListRooms() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	res := make([]string, 0, len(k.rooms))
	for roomID := range k.rooms {
		res = append(res, roomID)
	}
	slices.Sort(res)
	return res
}

func (k *Keeper) Subscribe(roomID string) (ch <-chan struct{}, cancel func(), ok bool) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	room, ok := k.rooms[roomID]
	if !ok {
		return nil, nil, false
	}
	ch, cancel = room.room.Subscribe()
	return ch, cancel, true
}

func (k *Keeper) RoomStateDelta(roomID string, old delta.Cursor) (*delta.State, delta.Cursor, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	room, ok := k.rooms[roomID]
	if !ok {
		return nil, delta.Cursor{}, &roomapi.Error{
			Code:    roomapi.ErrNoSuchRoom,
			Message: "no such room",
		}
	}
	d, cursor, err := room.room.StateDelta(old)
	if err != nil {
		return nil, delta.Cursor{}, fmt.Errorf("room state: %w", err)
	}
	return d, cursor, nil
}
