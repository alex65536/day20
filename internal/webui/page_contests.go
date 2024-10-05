package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/sliceutil"
	"github.com/alex65536/day20/internal/util/slogx"
)

type contestsDataBuilder struct{}

func (contestsDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log

	type item struct {
		ID       string
		Name     string
		Kind     scheduler.ContestKind
		Status   scheduler.ContestStatusKind
		Progress *progressPartData
		Result   string
	}

	type data struct {
		RunningOnly      bool
		CanStartContests bool
		Contests         []item
	}

	var contests []scheduler.ContestFullData
	runningOnly := req.URL.Query().Get("running") == "true"
	if runningOnly {
		contests = cfg.Scheduler.ListRunningContests()
	} else {
		var err error
		contests, err = cfg.Scheduler.ListAllContests(ctx)
		if err != nil {
			log.Warn("could not list all contests", slogx.Err(err))
			return nil, fmt.Errorf("list all contests: %w", err)
		}
	}
	slices.SortFunc(contests, func(a, b scheduler.ContestFullData) int {
		return strings.Compare(b.Info.ID, a.Info.ID)
	})

	canStartContests := false
	if bc.FullUser != nil && bc.FullUser.Perms.Get(userauth.PermRunContests) {
		canStartContests = true
	}

	return &data{
		RunningOnly:      runningOnly,
		CanStartContests: canStartContests,
		Contests: sliceutil.Map(contests, func(c scheduler.ContestFullData) item {
			if c.Info.Kind != scheduler.ContestMatch {
				panic("unknown contest kind")
			}
			return item{
				ID:       c.Info.ID,
				Name:     c.Info.Name,
				Kind:     c.Info.Kind,
				Status:   c.Data.Status.Kind,
				Progress: buildProgressPartData(c.Data.Match.Played(), c.Info.Match.Games),
				Result:   c.Data.Match.Status().ScoreString(),
			}
		}),
	}, nil
}

func contestsPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, contestsDataBuilder{}, "contests")
}
