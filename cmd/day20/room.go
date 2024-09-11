package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/alex65536/day20/internal/room"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var roomCmd = &cobra.Command{
	Use:   "room",
	Args:  cobra.ExactArgs(0),
	Short: "Start day20 room client",
}

func init() {
	defaultTokenFile := ""
	confDir, err := os.UserConfigDir()
	if err != nil {
		defaultTokenFile = filepath.Join(confDir, "day20", "token")
	}

	p := roomCmd.Flags()
	count := p.IntP(
		"count", "c", 1,
		"number of rooms to run")
	endpoint := p.StringP(
		"endpoint", "e", "",
		"room server endpoint")
	tokenFile := p.StringP(
		"token-file", "T", defaultTokenFile,
		"file with API token\n(ignored if DAY20_ROOM_TOKEN env is present)")

	roomCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		var token string
		if env := os.Getenv("DAY20_ROOM_TOKEN"); env != "" {
			token = env
		} else {
			data, err := os.ReadFile(*tokenFile)
			if err != nil {
				return fmt.Errorf("read token file: %w", err)
			}
			token = strings.TrimSpace(string(data))
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		// TODO: write neat colorful logs
		log := slog.Default()

		group, gctx := errgroup.WithContext(ctx)
		for range *count {
			group.Go(func() error {
				return room.Loop(gctx, log, room.Options{
					Client: roomapi.ClientOptions{
						Endpoint: *endpoint,
						Token:    token,
					},
				})
			})
		}

		if err := group.Wait(); err != nil {
			select {
			case <-ctx.Done():
			default:
				log.Error("fatal error", slogx.Err(err))
			}
		}
		return nil
	}
}
