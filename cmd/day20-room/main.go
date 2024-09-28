package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alex65536/day20/internal/enginemap"
	"github.com/alex65536/day20/internal/room"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var roomCmd = &cobra.Command{
	Use:     "day20-room",
	Args:    cobra.ExactArgs(0),
	Version: version.Version,
	Short:   "Start Day20 room client",
	Long: `Day20 is a toolkit to run and display confrontations between chess engines.

This command runs Day20 room client.
`,
}

func main() {
	p := roomCmd.Flags()
	optsPath := p.StringP(
		"options", "o", "",
		"options file",
	)
	if err := roomCmd.MarkFlagRequired("options"); err != nil {
		panic(err)
	}

	roomCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		var opts Options
		optsData, err := os.ReadFile(*optsPath)
		if err != nil {
			return fmt.Errorf("read options file: %w", err)
		}
		if err := toml.Unmarshal(optsData, &opts); err != nil {
			return fmt.Errorf("unmarshal options file: %w", err)
		}
		opts.FillDefaults()

		if opts.Rooms <= 0 {
			return fmt.Errorf("non-positive number of rooms")
		}
		if opts.URL == "" {
			return fmt.Errorf("room api url not specified in options")
		}
		if opts.Engines == nil {
			return fmt.Errorf("engine map not specified in options")
		}

		var token string
		if env := os.Getenv("DAY20_ROOM_TOKEN"); env != "" && opts.TokenFile == "" {
			token = strings.TrimSpace(env)
		} else {
			if opts.TokenFile == "" {
				confDir, err := os.UserConfigDir()
				if err != nil {
					return fmt.Errorf("could not locate token")
				}
				opts.TokenFile = filepath.Join(confDir, "day20", "token")
			}
			data, err := os.ReadFile(opts.TokenFile)
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
		for range opts.Rooms {
			group.Go(func() error {
				return room.Loop(gctx, log, room.Options{
					Client: roomapi.ClientOptions{
						Endpoint: opts.URL,
						Token:    token,
					},
				}, room.Config{
					EngineMap: enginemap.New(*opts.Engines),
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

	if err := roomCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
