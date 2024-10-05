package main

import (
	"context"
	"fmt"
	"log/slog"
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
	if err := serverCmd.MarkFlagRequired("options"); err != nil {
		panic(err)
	}

	serverCmd.RunE = func(cmd *cobra.Command, _args []string) error {
		rawOpts, err := os.ReadFile(*optsPath)
		if err != nil {
			return fmt.Errorf("read options: %w", err)
		}
		var opts Options
		if err := toml.Unmarshal(rawOpts, &opts); err != nil {
			return fmt.Errorf("unmarshal options: %w", err)
		}
		if err := opts.MixSecretsFromFile(); err != nil {
			return fmt.Errorf("mix secrets into options: %w", err)
		}
		opts.FillDefaults()

		serverCmd.SilenceUsage = true

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
		tokenChecker := userauth.NewTokenChecker(opts.TokenChecker, db)
		defer tokenChecker.Close()
		mux := http.NewServeMux()
		if err := roomapi.HandleServer(log, mux, "/api/room", keeper, roomapi.ServerConfig{
			TokenChecker: tokenChecker.Check,
		}); err != nil {
			return fmt.Errorf("handle server: %w", err)
		}
		webui.Handle(ctx, log, mux, "", webui.Config{
			Keeper:              keeper,
			UserManager:         userMgr,
			SessionStoreFactory: db,
			Scheduler:           scheduler,
		}, opts.WebUI)

		servers, err := newServers(ctx, log, &opts, mux)
		if err != nil {
			return fmt.Errorf("create servers: %w", err)
		}
		servers.Go()
		defer servers.Shutdown()

		<-ctx.Done()
		return nil
	}

	if err := serverCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
