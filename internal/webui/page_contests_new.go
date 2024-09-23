package webui

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/randutil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/clock"
	"github.com/gorilla/csrf"
)

type contestsNewData struct {
	CSRFField template.HTML
	Errors    []string
}

type contestsNewDataBuilder struct{}

func (contestsNewDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log
	user := bc.FullUser

	if user == nil || !user.Perms.Get(userauth.PermRunContests) {
		return nil, httputil.MakeError(http.StatusForbidden, "operation not permitted")
	}

	data := &contestsNewData{
		CSRFField: csrf.TemplateField(req),
	}

	switch req.Method {
	case http.MethodGet:
		return data, nil
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		var info scheduler.ContestInfo
		serr := func() string {
			var settings scheduler.ContestSettings
			settings.Name = req.FormValue("name")
			if settings.Name == "" {
				return "name not specified"
			}
			switch req.FormValue("time") {
			case "fixed":
				ms, err := strconv.ParseInt(req.FormValue("time-fixed-value"), 10, 64)
				if err != nil {
					return "no fixed time"
				}
				if ms > 1e9 {
					return "fixed time too large"
				}
				fixedTime := time.Duration(ms) * time.Millisecond
				settings.FixedTime = &fixedTime
			case "control":
				c, err := clock.ControlFromString(req.FormValue("time-control-value"))
				if err != nil {
					return "bad time control: " + err.Error()
				}
				settings.TimeControl = &c
			default:
				return "bad choice for time"
			}
			switch req.FormValue("openings") {
			case "gb20":
				settings.OpeningBook = scheduler.OpeningBook{
					Kind: scheduler.OpeningsBuiltin,
					Data: scheduler.BuiltinBookGBSelect2020,
				}
			case "gb14":
				settings.OpeningBook = scheduler.OpeningBook{
					Kind: scheduler.OpeningsBuiltin,
					Data: scheduler.BuiltinBookGraham20141F,
				}
			case "fen":
				settings.OpeningBook = scheduler.OpeningBook{
					Kind: scheduler.OpeningsFEN,
					Data: req.FormValue("openings-value"),
				}
			case "pgn-line":
				settings.OpeningBook = scheduler.OpeningBook{
					Kind: scheduler.OpeningsFEN,
					Data: req.FormValue("openings-value"),
				}
			default:
				return "bad opening kind"
			}
			if _, err := settings.OpeningBook.Book(randutil.DefaultSource()); err != nil {
				return "bad opening book: " + err.Error()
			}
			if t := req.FormValue("score-threshold"); t != "" {
				tv, err := strconv.ParseInt(t, 10, 32)
				if err != nil {
					return "bad score threshold"
				}
				settings.ScoreThreshold = int32(tv)
			}
			settings.Kind = scheduler.ContestMatch
			settings.Match = &scheduler.MatchSettings{}
			settings.Players = []roomapi.JobEngine{
				{Name: req.FormValue("first")},
				{Name: req.FormValue("second")},
			}
			for i, p := range settings.Players {
				if len(p.Name) == 0 {
					return fmt.Sprintf("no name for engine #%v", i+1)
				}
			}
			games, err := strconv.ParseInt(req.FormValue("games"), 10, 64)
			if err != nil {
				return "invalid number of games"
			}
			if games <= 0 {
				return "non-positive number of games"
			}
			settings.Match.Games = games
			localInfo, err := cfg.Scheduler.CreateContest(ctx, settings)
			if err != nil {
				log.Warn("failed to create contest", slogx.Err(err))
				return "failed to create contest"
			}
			info = localInfo
			return ""
		}()
		if serr != "" {
			data.Errors = []string{serr}
			return data, nil
		}
		return nil, httputil.MakeRedirectError(http.StatusSeeOther, "contest created", "/contest/"+info.ID)
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func contestsNewPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, contestsNewDataBuilder{}, "contests_new")
}
