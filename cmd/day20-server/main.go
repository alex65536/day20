package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/alex65536/day20/internal/database"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/version"
	"github.com/alex65536/day20/internal/webui"
)

var serverCmd = &cobra.Command{
	Use:     "server",
	Args:    cobra.ExactArgs(0),
	Version: version.Version,
	Short:   "Start Day20 server",
	Long: `Day20 is a toolkit to run and display confrontations between chess engines.

This command runs Day20 server.
`,
}

func main() {
	p := serverCmd.Flags()
	optsPath := p.StringP(
		"options", "o", "",
		"options file",
	)
	secretsPath := p.StringP(
		"secrets", "s", "",
		"secrets file",
	)
	if err := serverCmd.MarkFlagRequired("options"); err != nil {
		panic(err)
	}
	if err := serverCmd.MarkFlagRequired("secrets"); err != nil {
		panic(err)
	}

	serverCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		rawSecrets, err := os.ReadFile(*secretsPath)
		if err != nil {
			rawSecrets = nil
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read secrets: %w", err)
			}
		}
		var secrets Secrets
		if err := toml.Unmarshal(rawSecrets, &secrets); err != nil {
			return fmt.Errorf("unmarshal secrets")
		}
		secretsChanged, err := secrets.GenerateMissing()
		if err != nil {
			return fmt.Errorf("generate secrets: %w", err)
		}
		if secretsChanged {
			newRawSecrets, err := toml.Marshal(&secrets)
			if err != nil {
				return fmt.Errorf("marshal secrets")
			}
			if err := os.WriteFile(*secretsPath, newRawSecrets, 0600); err != nil {
				return fmt.Errorf("write secrets: %w", err)
			}
		}

		rawOpts, err := os.ReadFile(*optsPath)
		if err != nil {
			return fmt.Errorf("read options: %w", err)
		}
		var opts Options
		if err := toml.Unmarshal(rawOpts, &opts); err != nil {
			return fmt.Errorf("unmarshal options: %w", err)
		}
		if err := opts.MixSecrets(&secrets); err != nil {
			return fmt.Errorf("mix secrets into options: %w", err)
		}
		opts.FillDefaults()

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		// TODO: write neat colorful logs
		log := slog.Default()

		db, err := database.New(log, opts.DB)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()
		userMgr, err := userauth.NewManager(log, db, opts.Users)
		if err != nil {
			return fmt.Errorf("create user manager: %w", err)
		}
		defer userMgr.Close()
		scheduler, err := scheduler.New(ctx, log, db, opts.Scheduler)
		if err != nil {
			return fmt.Errorf("create scheduler: %w", err)
		}
		keeper, err := roomkeeper.New(ctx, log, db, scheduler, opts.RoomKeeper)
		if err != nil {
			return fmt.Errorf("create roomkeeper: %w", err)
		}
		defer keeper.Close()
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
		webui.Handle(ctx, log, mux, "", webui.Config{
			Keeper:              keeper,
			UserManager:         userMgr,
			SessionStoreFactory: db,
			Scheduler:           scheduler,
		}, opts.WebUI)

		servFin := make(chan struct{})
		servCtx, servCancel := context.WithCancel(ctx)
		server := &http.Server{
			Addr:        opts.Addr,
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

	if err := serverCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
