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
	engineMapText := p.StringP(
		"engine-map", "M", "",
		"engine map (inline, in toml format)",
	)
	// TODO: invent a single config
	engineMapFile := p.StringP(
		"engine-map-file", "m", "",
		"engine map file (in toml format)",
	)
	roomCmd.MarkFlagsMutuallyExclusive("engine-map", "engine-map-file")
	roomCmd.MarkFlagsOneRequired("engine-map", "engine-map-file")

	roomCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		var token string
		if *tokenFile != "" {
			data, err := os.ReadFile(*tokenFile)
			if err != nil {
				return fmt.Errorf("read token file: %w", err)
			}
			token = strings.TrimSpace(string(data))
		} else if env := os.Getenv("DAY20_ROOM_TOKEN"); env != "" {
			token = env
		} else {
			return fmt.Errorf("token not specified")
		}

		var engineMapOpts enginemap.Options
		if *engineMapFile != "" {
			data, err := os.ReadFile(*engineMapFile)
			if err != nil {
				return fmt.Errorf("read engine map file: %w", err)
			}
			if err := toml.Unmarshal(data, &engineMapOpts); err != nil {
				return fmt.Errorf("unmarshal engine map: %w", err)
			}
		} else if *engineMapText != "" {
			if err := toml.Unmarshal([]byte(*engineMapText), &engineMapOpts); err != nil {
				return fmt.Errorf("unmarshal engine map: %w", err)
			}
		} else {
			return fmt.Errorf("neither engine map file nor engine map specified")
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
				}, room.Config{
					EngineMap: enginemap.New(engineMapOpts),
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
