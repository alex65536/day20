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

type contestsDataItem struct {
	ID       string
	Name     string
	Kind     string
	Status   string
	Progress float64
	Result   string
}

type contestsData struct {
	All              bool
	CanStartContests bool
	Contests         []contestsDataItem
}

type contestsDataBuilder struct{}

func (contestsDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log

	var contests []scheduler.ContestFullData
	all := req.URL.Query().Get("all") == "true"
	if all {
		var err error
		contests, err = cfg.Scheduler.ListAllContests(ctx)
		if err != nil {
			log.Warn("could not list all contests", slogx.Err(err))
			return nil, fmt.Errorf("list all contests: %w", err)
		}
	} else {
		contests = cfg.Scheduler.ListRunningContests()
	}
	slices.SortFunc(contests, func(a, b scheduler.ContestFullData) int {
		return strings.Compare(b.Info.ID, a.Info.ID)
	})

	canStartContests := false
	if bc.FullUser != nil && bc.FullUser.Perms.Get(userauth.PermRunContests) {
		canStartContests = true
	}

	return &contestsData{
		All:              all,
		CanStartContests: canStartContests,
		Contests: sliceutil.Map(contests, func(c scheduler.ContestFullData) contestsDataItem {
			it := contestsDataItem{
				ID:     c.Info.ID,
				Name:   c.Info.Name,
				Kind:   c.Info.Kind.PrettyString(),
				Status: c.Data.Status.Kind.PrettyString(),
			}
			switch c.Info.Kind {
			case scheduler.ContestMatch:
				if c.Info.Match.Games == 0 {
					it.Progress = -1.0
				} else {
					it.Progress = float64(c.Data.Match.Played()) / float64(c.Info.Match.Games) * 100
				}
				it.Result = c.Data.Match.Status().ScoreString()
			default:
				panic("unknown contest kind")
			}
			return it
		}),
	}, nil
}

func contestsPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, contestsDataBuilder{}, "contests")
}
