package room

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/enginemap"
	"github.com/alex65536/day20/internal/opening"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/backoff"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
)

type Options struct {
	Client          roomapi.ClientOptions
	JobPollDuration time.Duration
	ByeTimeout      time.Duration
	RequestTimeout  time.Duration
	Backoff         backoff.Options
	Watcher         delta.WatcherOptions
	PingInterval    time.Duration
}

type Config struct {
	EngineMap enginemap.Map
}

func (o *Options) FillDefaults() {
	if o.JobPollDuration <= 0 {
		o.JobPollDuration = 30 * time.Second
	}
	if o.ByeTimeout <= 0 {
		o.ByeTimeout = 1 * time.Second
	}
	if o.RequestTimeout <= 0 {
		o.RequestTimeout = 10 * time.Second
	}
	if o.PingInterval == 0 {
		o.PingInterval = 3 * time.Second
	}
}

func requestWithTimeout[Req, Rsp any](
	ctx context.Context,
	timeout time.Duration,
	method func(context.Context, *Req) (*Rsp, error),
	req *Req,
) (*Rsp, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return method(ctx, req)
}

func retryBackoff(ctx context.Context, b *backoff.Backoff, err error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if !roomapi.IsErrorRetriable(err) {
		return err
	}
	return b.Retry(ctx, err)
}

type job struct {
	client roomapi.API
	o      *Options
	desc   *roomapi.Job
	roomID string
	log    *slog.Logger
	mp     enginemap.Map
}

func newJob(client roomapi.API, o *Options, cfg *Config, desc *roomapi.Job, roomID string, log *slog.Logger) *job {
	return &job{
		client: client,
		o:      o,
		desc:   desc,
		roomID: roomID,
		log:    log.With(slog.String("job_id", desc.ID)),
		mp:     cfg.EngineMap,
	}
}

func (j *job) update(ctx context.Context, upd *roomapi.UpdateRequest) error {
	backoff, err := backoff.New(j.o.Backoff)
	if err != nil {
		return fmt.Errorf("create backoff: %w", err)
	}
	for {
		_, err := requestWithTimeout(ctx, j.o.RequestTimeout, j.client.Update, upd)
		if err != nil {
			j.log.Info("error sending update", slogx.Err(err))
			if err := retryBackoff(ctx, backoff, err); err != nil {
				return fmt.Errorf("update job: %w", err)
			}
			continue
		}
		return nil
	}
}

func (j *job) prefail(ctx context.Context, failErr error) error {
	return j.update(ctx, &roomapi.UpdateRequest{
		RoomID: j.roomID,
		From:   delta.JobCursor{},
		Delta:  &delta.JobState{},
		Done:   true,
		Error:  failErr.Error(),
	})
}

func (j *job) closeBattle(battle *battle.Battle) {
	battle.White.Close()
	battle.Black.Close()
}

func (j *job) makeBattle(ctx context.Context) (*battle.Battle, error) {
	opts := battle.Options{
		ScoreThreshold: j.desc.ScoreThreshold,
	}
	if j.desc.TimeMargin != nil {
		opts.DeadlineMargin = maybe.Some(*j.desc.TimeMargin)
	}
	if j.desc.FixedTime != nil {
		opts.FixedTime = maybe.Some(*j.desc.FixedTime)
	}
	if j.desc.TimeControl != nil {
		opts.TimeControl = maybe.Some(j.desc.TimeControl.Clone())
	}

	var game *chess.Game
	if j.desc.StartBoard != nil {
		b, err := chess.NewBoard(*j.desc.StartBoard)
		if err != nil {
			return nil, fmt.Errorf("create start board: %w", err)
		}
		game = chess.NewGameWithPosition(b)
	} else {
		game = chess.NewGame()
	}
	for i, mv := range j.desc.StartMoves {
		if err := game.PushUCIMove(mv); err != nil {
			return nil, fmt.Errorf("apply start move %d: %w", i+1, err)
		}
	}
	book := opening.NewSingleGameBook(game)

	wopts, err := j.mp.GetOptions(j.desc.White)
	if err != nil {
		return nil, fmt.Errorf("cannot get white options: %w", err)
	}
	wpool, err := battle.NewEnginePool(ctx, j.log.With(slog.String("color", "white")), wopts)
	if err != nil {
		return nil, fmt.Errorf("create white pool: %w", err)
	}
	defer func() {
		if wpool != nil {
			wpool.Close()
		}
	}()

	bopts, err := j.mp.GetOptions(j.desc.Black)
	if err != nil {
		return nil, fmt.Errorf("cannot get black options: %w", err)
	}
	bpool, err := battle.NewEnginePool(ctx, j.log.With(slog.String("color", "black")), bopts)
	if err != nil {
		return nil, fmt.Errorf("create black pool: %w", err)
	}
	defer func() {
		if bpool != nil {
			bpool.Close()
		}
	}()

	b := &battle.Battle{
		White:   wpool,
		Black:   bpool,
		Book:    book,
		Options: opts,
	}
	wpool = nil
	bpool = nil
	return b, nil
}

func (j *job) watchUpdates(ctx context.Context, watcher *delta.Watcher, upd <-chan struct{}, onFinish func()) <-chan error {
	updateCh := make(chan error, 1)
	go func() {
		defer onFinish()

		updateCh <- func() error {
			cursor := delta.JobCursor{}

			doSend := func(done bool) error {
				var emptyCursor delta.JobCursor
				for {
					dd, newCursor, err := watcher.StateDelta(cursor)
					if err != nil {
						panic(fmt.Sprintf("must not happen: %v", err))
					}
					if err := j.update(ctx, &roomapi.UpdateRequest{
						RoomID:    j.roomID,
						From:      cursor,
						Delta:     dd,
						Timestamp: delta.NowTimestamp(),
						Done:      done,
					}); err != nil {
						if roomapi.MatchesError(err, roomapi.ErrNeedsResync) && cursor != emptyCursor {
							cursor = emptyCursor
							continue
						}
						return fmt.Errorf("send update: %w", err)
					}
					cursor = newCursor
					return nil
				}
			}

			ticker := time.NewTicker(j.o.PingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-watcher.Done():
					if err := doSend(true); err != nil {
						return err
					}
					return nil
				case <-ticker.C:
					if err := doSend(false); err != nil {
						return err
					}
				case <-upd:
					if err := doSend(false); err != nil {
						return err
					}
				}
			}
		}()
	}()
	return updateCh
}

func (j *job) Do(ctx context.Context) error {
	j.log.Info("starting job")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	battle, err := j.makeBattle(ctx)
	if err != nil {
		j.log.Error("cannot make battle", slogx.Err(err))
		if err := j.prefail(ctx, fmt.Errorf("make battle: %w", err)); err != nil {
			return fmt.Errorf("prefail: %w", err)
		}
		return nil
	}
	defer j.closeBattle(battle)

	watcher, upd := delta.NewWatcher(j.o.Watcher)
	defer watcher.Close()

	battleCtx, battleCancel := context.WithCancel(ctx)
	defer battleCancel()
	updateCh := j.watchUpdates(ctx, watcher, upd, battleCancel)

	game, warn, err := battle.Do(battleCtx, watcher)
	watcher.Close()
	if err != nil {
		<-updateCh
		j.log.Error("cannot run battle", slogx.Err(err))
		if err := j.prefail(ctx, fmt.Errorf("run battle: %w", err)); err != nil {
			return fmt.Errorf("prefail: %w", err)
		}
		return nil
	}
	err = <-updateCh
	if err != nil {
		return fmt.Errorf("send updates: %w", err)
	}

	{
		// Validation.
		stateDelta, _, err := watcher.StateDelta(delta.JobCursor{})
		if err != nil {
			panic(fmt.Sprintf("watcher state delta: %v", err))
		}
		allState := delta.NewJobState()
		if err := allState.ApplyDelta(stateDelta); err != nil {
			panic(fmt.Sprintf("apply state delta: %v", err))
		}
		gameFromState, err := allState.GameExt()
		if err != nil {
			panic(fmt.Sprintf("state contains corrupted game: %v", err))
		}
		if !reflect.DeepEqual(game, gameFromState) {
			panic("real game diverged from the state")
		}
		if !slices.Equal(warn, allState.Warnings.Warn) {
			panic("real warnings diverged from the state")
		}
	}

	return nil
}

type room struct {
	client roomapi.API
	o      *Options
	cfg    *Config
	roomID string
}

func (r *room) Do(ctx context.Context, log *slog.Logger) error {
	log = log.With(slog.String("room_id", r.roomID))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer r.bye(log)

	log.Info("room started")
	backoff, err := backoff.New(r.o.Backoff)
	if err != nil {
		return fmt.Errorf("create backoff: %w", err)
	}
	for {
		rsp, err := func() (*roomapi.JobResponse, error) {
			rsp, err := requestWithTimeout(
				ctx,
				r.o.JobPollDuration+r.o.RequestTimeout,
				r.client.Job,
				&roomapi.JobRequest{
					RoomID:  r.roomID,
					Timeout: r.o.JobPollDuration,
				},
			)
			if err != nil {
				return nil, fmt.Errorf("job: %w", err)
			}
			return rsp, nil
		}()
		if err != nil {
			if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
				r.roomID = ""
				log.Warn("room expired")
				return nil
			}
			if roomapi.MatchesError(err, roomapi.ErrNoJob) {
				continue
			}
			log.Warn("error waiting for job", slogx.Err(err))
			if err := retryBackoff(ctx, backoff, err); err != nil {
				return fmt.Errorf("wait for job: %w", err)
			}
			continue
		}
		backoff.Reset()

		if err := func() error {
			job := newJob(r.client, r.o, r.cfg, &rsp.Job, r.roomID, log)
			if err := job.Do(ctx); err != nil {
				return fmt.Errorf("do job: %w", err)
			}
			return nil
		}(); err != nil {
			if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
				r.roomID = ""
				log.Warn("room expired")
				return nil
			}
			if roomapi.MatchesError(err, roomapi.ErrNoJobRunning) {
				continue
			}
			log.Warn("error running job", slogx.Err(err))
			return nil
		}
	}
}

func (r *room) bye(log *slog.Logger) {
	if r.roomID == "" {
		return
	}

	log.Info("leaving room")
	if _, err := requestWithTimeout(
		context.Background(),
		r.o.ByeTimeout,
		r.client.Bye,
		&roomapi.ByeRequest{RoomID: r.roomID},
	); err != nil {
		log.Warn("error saying bye", slogx.Err(err))
	}
}

func Loop(ctx context.Context, log *slog.Logger, o Options, cfg Config) error {
	o.FillDefaults()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Info("room loop started")
	client := roomapi.NewClient(o.Client, http.DefaultClient)
	backoff, err := backoff.New(o.Backoff)
	if err != nil {
		return fmt.Errorf("create backoff: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rsp, err := requestWithTimeout(
			ctx,
			o.RequestTimeout,
			client.Hello,
			&roomapi.HelloRequest{
				SupportedProtoVersions: []int32{roomapi.ProtoVersion},
			},
		)
		if err != nil {
			log.Warn("error saying hello", slogx.Err(err))
			if err := retryBackoff(ctx, backoff, err); err != nil {
				return fmt.Errorf("saying hello: %w", err)
			}
			continue
		}
		if rsp.ProtoVersion != roomapi.ProtoVersion {
			return fmt.Errorf("unsupported proto version")
		}
		r := &room{
			client: client,
			o:      &o,
			cfg:    &cfg,
			roomID: rsp.RoomID,
		}
		if err := r.Do(ctx, log); err != nil {
			log.Warn("room failed", slogx.Err(err))
			return fmt.Errorf("run room: %w", err)
		}
	}
}
