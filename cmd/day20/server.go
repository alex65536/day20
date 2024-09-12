package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	randutil "github.com/alex65536/day20/internal/util/rand"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/clock"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Args:  cobra.ExactArgs(0),
	Short: "Start day20 server",
}

type db struct{}

func (db) UpdateRoom(context.Context, roomkeeper.RoomDesc) error { return nil }
func (db) DeleteRoom(context.Context, string) error              { return nil }
func (db) AddGame(_ context.Context, _ string, game *battle.GameExt) error {
	pgn, err := game.PGN()
	if err != nil {
		fmt.Println("bad game: " + err.Error())
		return fmt.Errorf("bad game: %w", err)
	}
	fmt.Println(pgn)
	return nil
}

type scheduler struct {
	log *slog.Logger
}

func (scheduler) IsContestRunning(contestID string) bool {
	return true
}

var globalControl clock.Control

func (scheduler) NextJob(ctx context.Context) (*roomkeeper.Job, error) {
	return &roomkeeper.Job{
		JobID:     randutil.InsecureID(),
		ContestID: "contest0",
		Desc: roomapi.Job{
			TimeControl: &globalControl,
			White: roomapi.JobEngine{
				Name: "stockfish",
			},
			Black: roomapi.JobEngine{
				Name: "stockfish",
			},
		},
	}, nil
}

func (s scheduler) OnJobFinished(jobID string, status roomkeeper.JobStatus) {
	s.log.Info("job finished",
		slog.String("job_id", jobID),
		slog.String("status", status.Kind.String()),
		slog.String("reason", status.Reason),
	)
}

func init() {
	p := serverCmd.Flags()
	endpoint := p.StringP(
		"endpoint", "e", "127.0.0.1:8080",
		"server endpoint")
	control := p.StringP(
		"time-control", "C", "40/60",
		"time control")

	serverCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		var err error
		globalControl, err = clock.ControlFromString(*control)
		if err != nil {
			return fmt.Errorf("parse control: %w", err)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		// TODO: write neat colorful logs
		log := slog.Default()

		keeper := roomkeeper.New(log, db{}, scheduler{log: log}, roomkeeper.Options{}, nil)
		mux := http.NewServeMux()
		if err := roomapi.RegisterServer(keeper, mux, roomapi.ServerOptions{
			TokenChecker: func(token string) error {
				if token != "test" {
					return fmt.Errorf("bad token")
				}
				return nil
			},
		}, "", log); err != nil {
			return fmt.Errorf("register server: %w", err)
		}

		servFin := make(chan struct{})
		servCtx, servCancel := context.WithCancel(ctx)
		server := &http.Server{
			Addr:        *endpoint,
			Handler:     mux,
			BaseContext: func(net.Listener) context.Context { return servCtx },
		}
		go func() {
			defer close(servFin)
			log.Info("starting http server")
			if err := server.ListenAndServe(); err != nil {
				select {
				case <-servCtx.Done():
				default:
					log.Warn("listen http server failed", slogx.Err(err))
				}
			}
		}()
		defer func() { <-servFin }()
		defer func() {
			log.Info("stopping server")
			servCancel()
			_ = server.Shutdown(servCtx)
		}()

		for {
			rooms := keeper.ListRooms()
			if len(rooms) == 0 {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			ch, _, ok := keeper.Subscribe(rooms[0])
			if !ok {
				log.Info("cannot subscribe to room")
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			log.Info("successfully subscribed to room")
			state := delta.NewState()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ch:
				}
				d, _, err := keeper.RoomStateDelta(rooms[0], state.Cursor())
				if err != nil {
					if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
						break
					}
					if roomapi.MatchesError(err, roomapi.ErrNoJobRunning) {
						state = delta.NewState()
						continue
					}
					log.Warn("error getting state", slogx.Err(err))
					break
				}
				if err := state.ApplyDelta(d); err != nil {
					log.Error("cannot apply delta", slogx.Err(err))
					break
				}
				ds, err := json.Marshal(d)
				if err != nil {
					log.Error("cannot marshal delta", slogx.Err(err))
					break
				}
				now := delta.NowTimestamp()
				fmt.Printf("got delta: %v\n", string(ds))
				fmt.Printf("new clock: %v - %v\n", state.White.ClockFrom(now).Get(), state.Black.ClockFrom(now).Get())
			}
		}
	}
}
