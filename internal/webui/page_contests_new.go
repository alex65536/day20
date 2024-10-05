package webui

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/randutil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/clock"
	"github.com/gorilla/csrf"
)

type contestsNewDataBuilder struct{}

func (contestsNewDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log
	user := bc.FullUser

	type data struct {
		CSRFField template.HTML
	}

	if user == nil || !user.Perms.Get(userauth.PermRunContests) {
		return nil, httputil.MakeError(http.StatusForbidden, "operation not permitted")
	}

	switch req.Method {
	case http.MethodGet:
		return &data{
			CSRFField: csrf.TemplateField(req),
		}, nil
	case http.MethodPost:
		if !bc.IsHTMX() {
			return nil, httputil.MakeError(http.StatusBadRequest, "must use htmx request")
		}
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		var info scheduler.ContestInfo
		errs := func() []string {
			var errs []string
			var settings scheduler.ContestSettings

			settings.Name = req.FormValue("name")
			if settings.Name == "" {
				errs = append(errs, "name not specified")
			} else if utf8.RuneCountInString(settings.Name) > scheduler.ContestNameMaxLen {
				errs = append(errs, fmt.Sprintf("name exceeds %v runes", scheduler.ContestNameMaxLen))
			}

			switch req.FormValue("time") {
			case "fixed":
				ms, err := strconv.ParseInt(req.FormValue("time-fixed-value"), 10, 64)
				if err != nil {
					errs = append(errs, "no fixed time")
					break
				}
				if ms > 1e9 {
					errs = append(errs, "fixed time too large")
					break
				}
				fixedTime := time.Duration(ms) * time.Millisecond
				settings.FixedTime = &fixedTime
			case "control":
				c, err := clock.ControlFromString(req.FormValue("time-control-value"))
				if err != nil {
					errs = append(errs, "bad time control: "+err.Error())
					break
				}
				settings.TimeControl = &c
			default:
				errs = append(errs, "bad choice for time")
			}

			hasBook := true
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
					Kind: scheduler.OpeningsPGNLine,
					Data: req.FormValue("openings-value"),
				}
			default:
				errs = append(errs, "bad opening kind")
				hasBook = false
			}
			if hasBook {
				if _, err := settings.OpeningBook.Book(randutil.DefaultSource()); err != nil {
					errs = append(errs, "bad opening book: "+err.Error())
				}
			}

			if t := req.FormValue("score-threshold"); t != "" {
				tv, err := strconv.ParseInt(t, 10, 32)
				if err != nil {
					errs = append(errs, "bad score threshold")
				} else {
					settings.ScoreThreshold = int32(tv)
				}
			}

			settings.Kind = scheduler.ContestMatch
			settings.Match = &scheduler.MatchSettings{}

			settings.Players = []roomapi.JobEngine{
				{Name: req.FormValue("first")},
				{Name: req.FormValue("second")},
			}
			for i, p := range settings.Players {
				if len(p.Name) == 0 {
					errs = append(errs, fmt.Sprintf("no name for engine #%v", i+1))
				}
			}

			games, err := strconv.ParseInt(req.FormValue("games"), 10, 64)
			if err != nil {
				errs = append(errs, "invalid number of games")
			} else if games <= 0 {
				errs = append(errs, "non-positive number of games")
			} else {
				settings.Match.Games = games
			}

			if len(errs) != 0 {
				return errs
			}

			err = settings.Validate()
			if err != nil {
				return []string{err.Error()}
			}

			info, err = cfg.Scheduler.CreateContest(ctx, settings)
			if err != nil {
				log.Warn("failed to create contest", slogx.Err(err))
				return []string{"failed to create contest"}
			}
			return nil
		}()
		if len(errs) != 0 {
			return &errorsPartData{Errors: errs}, nil
		}
		return nil, bc.Redirect("/contest/" + info.ID)
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func contestsNewPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, contestsNewDataBuilder{}, "contests_new")
}
