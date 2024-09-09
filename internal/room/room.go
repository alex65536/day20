package room

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/opening"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/backoff"
	randutil "github.com/alex65536/day20/internal/util/rand"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type Options struct {
	Client              roomapi.ClientOptions
	JobPollDuration     time.Duration
	ByeTimeout          time.Duration
	RequestTimeout      time.Duration
	Backoff             backoff.Options
	Watcher             delta.WatcherOptions
	EngineOptions       uci.EngineOptions
	EngineCreateTimeout maybe.Maybe[time.Duration]
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
}

type job struct {
	client *roomapi.Client
	o      *Options
	desc   *roomapi.Job
	roomID string
	log    *slog.Logger
}

func newJob(client *roomapi.Client, o *Options, desc *roomapi.Job, roomID string, log *slog.Logger) *job {
	return &job{
		client: client,
		o:      o,
		desc:   desc,
		roomID: roomID,
		log:    log.With(slog.String("job_id", randutil.InsecureID())),
	}
}

func (j *job) update(ctx context.Context, upd *roomapi.UpdateRequest) error {
	backoff, err := backoff.New(j.o.Backoff)
	if err != nil {
		return fmt.Errorf("create backoff: %w", err)
	}
	for {
		_, err := j.client.Update(ctx, upd)
		if err != nil {
			if apiErr := (*roomapi.Error)(nil); errors.As(err, &apiErr) {
				return fmt.Errorf("update job: %w", err)
			}
			j.log.Warn("error sending update", slogx.Err(err))
			if err := backoff.Retry(ctx, err); err != nil {
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
		From:   delta.Cursor{},
		Delta:  &delta.State{},
		Done:   true,
		Error:  failErr.Error(),
	})
}

func (j *job) makePoolOptions(e roomapi.JobEngine) battle.EnginePoolOptions {
	return battle.EnginePoolOptions{
		Name:          e.Name,
		Args:          nil,
		Options:       nil,
		EngineOptions: j.o.EngineOptions.Clone(),
		CreateTimeout: j.o.EngineCreateTimeout,
	}
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

	wpool, err := battle.NewEnginePool(ctx, j.log.With(slog.String("color", "white")), j.makePoolOptions(j.desc.White))
	if err != nil {
		return nil, fmt.Errorf("create white pool: %w", err)
	}
	defer func() {
		if wpool != nil {
			wpool.Close()
		}
	}()

	bpool, err := battle.NewEnginePool(ctx, j.log.With(slog.String("color", "black")), j.makePoolOptions(j.desc.Black))
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

func (j *job) watchUpdates(ctx context.Context, watcher *delta.Watcher, upd <-chan struct{}) <-chan error {
	updateCh := make(chan error, 1)
	go func() {
		updateCh <- func() error {
			cursor := delta.Cursor{}

			doSend := func(done bool) error {
				var emptyCursor delta.Cursor
				for {
					delta, newCursor, err := watcher.State(cursor)
					if err != nil {
						panic(fmt.Sprintf("must not happen: %v", err))
					}
					if err := j.update(ctx, &roomapi.UpdateRequest{
						RoomID: j.roomID,
						From:   cursor,
						Delta:  delta,
						Done:   done,
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

			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-watcher.Done():
					if err := doSend(true); err != nil {
						return err
					}
					return nil
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

func (j *job) do(ctx context.Context) error {
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

	watcher, upd := delta.NewWatcher(j.o.Watcher)
	defer watcher.Close()

	updateCh := j.watchUpdates(ctx, watcher, upd)

	game, warn, err := battle.Do(ctx, watcher)
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
		j.log.Error("cannot send updates", slogx.Err(err))
		return fmt.Errorf("send updates: %w", err)
	}

	{
		// Validation.
		allState, _, err := watcher.State(delta.Cursor{})
		if err != nil {
			panic(fmt.Sprintf("cannot get state: %v", err))
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
	client *roomapi.Client
	o      *Options
	roomID string
}

func (r *room) do(ctx context.Context, log *slog.Logger) error {
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
			subctx, cancel := context.WithTimeout(ctx, r.o.JobPollDuration+r.o.RequestTimeout)
			defer cancel()
			rsp, err := r.client.Job(subctx, &roomapi.JobRequest{
				RoomID:  r.roomID,
				Timeout: r.o.JobPollDuration,
			})
			if err != nil {
				return nil, fmt.Errorf("job: %w", err)
			}
			return rsp, nil
		}()
		if err != nil {
			if apiErr := (*roomapi.Error)(nil); errors.As(err, &apiErr) {
				switch apiErr.Code {
				case roomapi.ErrNoSuchRoom:
					r.roomID = ""
					log.Warn("room expired")
					return nil
				case roomapi.ErrNoJob:
					continue
				default:
					log.Warn("error waiting for job", slogx.Err(err))
					return fmt.Errorf("waiting for job: %w", err)
				}
			}
			log.Warn("error waiting for job", slogx.Err(err))
			if err := backoff.Retry(ctx, err); err != nil {
				return fmt.Errorf("wait for job: %w", err)
			}
			continue
		}
		backoff.Reset()

		if err := func() error {
			job := newJob(r.client, r.o, &rsp.Job, r.roomID, log)
			if err := job.do(ctx); err != nil {
				return fmt.Errorf("do job: %w", err)
			}
			return nil
		}(); err != nil {
			if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
				r.roomID = ""
				log.Warn("room expired")
				return nil
			}
			if roomapi.MatchesError(err, roomapi.ErrJobCanceled) {
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

	ctx, cancel := context.WithTimeout(context.Background(), r.o.ByeTimeout)
	defer cancel()
	log.Info("leaving room")
	if _, err := r.client.Bye(ctx, &roomapi.ByeRequest{RoomID: r.roomID}); err != nil {
		log.Warn("error saying bye", slogx.Err(err))
	}
}

func Loop(ctx context.Context, log *slog.Logger, o Options) error {
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
		rsp, err := client.Hello(ctx, &roomapi.HelloRequest{})
		if err != nil {
			log.Warn("error saying hello", slogx.Err(err))
			if err := backoff.Retry(ctx, err); err != nil {
				return fmt.Errorf("saying hello: %w", err)
			}
			continue
		}
		r := &room{
			client: client,
			o:      &o,
			roomID: rsp.RoomID,
		}
		if err := r.do(ctx, log); err != nil {
			log.Warn("room failed", slogx.Err(err))
			return fmt.Errorf("run room: %w", err)
		}
	}
}
