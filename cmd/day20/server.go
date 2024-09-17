package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/webui"
	"github.com/alex65536/go-chess/clock"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Args:  cobra.ExactArgs(0),
	Short: "Start day20 server",
}

type db struct{}

func (db) ListActiveRooms(context.Context) ([]roomkeeper.RoomFullData, error) { return nil, nil }
func (db) CreateRoom(context.Context, roomkeeper.RoomInfo) error              { return nil }
func (db) UpdateRoom(context.Context, string, roomkeeper.RoomData) error      { return nil }
func (db) DeleteRoom(context.Context, string) error                           { return nil }

type scheduler struct {
	log   *slog.Logger
	first *atomic.Bool
}

func newScheduler(log *slog.Logger) *scheduler {
	return &scheduler{
		log:   log,
		first: new(atomic.Bool),
	}
}

func (s *scheduler) IsJobAborted(jobID string) (string, bool) {
	return "", false
}

var globalControl clock.Control

func (s *scheduler) NextJob(ctx context.Context) (*roomkeeper.Job, error) {
	if s.first.Swap(true) {
		s.log.Info("sleeping before new job")
		time.Sleep(3 * time.Second)
	}
	return &roomkeeper.Job{
		Desc: roomapi.Job{
			ID:          idgen.ID(),
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

func (s scheduler) OnJobFinished(jobID string, status roomkeeper.JobStatus, game *battle.GameExt) {
	s.log.Info("job finished",
		slog.String("job_id", jobID),
		slog.String("status", status.Kind.String()),
		slog.String("reason", status.Reason),
	)
	if status.Kind == roomkeeper.JobSucceeded {
		pgn, err := game.PGN()
		if err != nil {
			fmt.Println("bad game: " + err.Error())
		} else {
			fmt.Println(pgn)
		}
	}
}

func init() {
	p := serverCmd.Flags()
	endpoint := p.StringP(
		"endpoint", "e", "127.0.0.1:8080",
		"server endpoint")
	control := p.StringP(
		"time-control", "C", "40/20",
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

		keeper, err := roomkeeper.New(ctx, log, db{}, newScheduler(log), roomkeeper.Options{})
		if err != nil {
			return fmt.Errorf("create roomkeeper: %w", err)
		}
		mux := http.NewServeMux()
		if err := roomapi.HandleServer(log, mux, "/api/room", keeper, roomapi.ServerOptions{
			TokenChecker: func(token string) error {
				if token != "test" {
					return fmt.Errorf("bad token")
				}
				return nil
			},
		}); err != nil {
			return fmt.Errorf("handle server: %w", err)
		}
		webui.Handle(log, mux, "", webui.Config{
			Keeper: keeper,
		}, webui.Options{})

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

		<-ctx.Done()
		return nil
	}
}
