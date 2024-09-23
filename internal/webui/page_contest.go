package webui

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/stat"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/gorilla/csrf"
)

type contestData struct {
	ID               string
	Name             string
	Kind             string
	Status           string
	Reason           string
	Progress         float64
	FirstWin         int
	Draw             int
	SecondWin        int
	Score            string
	Winner           string
	WinnerConfidence float64
	LOS              float64
	EloDiff          stat.EloDiff
	CanCancel        bool
	CSRFField        template.HTML
	FixedTime        string
	TimeControl      string
	ScoreThreshold   int32
	First            string
	Second           string
	Total            int
}

type contestDataBuilder struct{}

func (contestDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log

	info, data, err := cfg.Scheduler.GetContest(ctx, req.PathValue("contestID"))
	if err != nil {
		log.Info("could not get contest", slogx.Err(err))
		return nil, httputil.MakeError(http.StatusNotFound, "contest not found")
	}
	canCancel := bc.FullUser != nil && bc.FullUser.Perms.Get(userauth.PermRunContests)

	switch req.Method {
	case http.MethodGet:
		switch info.Kind {
		case scheduler.ContestMatch:
			var progress float64
			if info.Match.Games == 0 {
				progress = -1.0
			} else {
				progress = float64(data.Match.Played()) / float64(info.Match.Games) * 100
			}
			status := data.Match.Status()
			confidence, winner := status.Winner(0.9, 0.95, 0.97, 0.99)
			fixedTime, timeControl := "", ""
			if info.FixedTime != nil {
				fixedTime = info.FixedTime.String()
			}
			if info.TimeControl != nil {
				timeControl = info.TimeControl.String()
			}
			return &contestData{
				ID:        info.ID,
				Name:      info.Name,
				Kind:      info.Kind.PrettyString(),
				Status:    data.Status.Kind.PrettyString(),
				Reason:    data.Status.Reason,
				Progress:  progress,
				FirstWin:  int(data.Match.FirstWin),
				Draw:      int(data.Match.Draw),
				SecondWin: int(data.Match.SecondWin),
				Score:     status.ScoreString(),
				Winner: (map[stat.Winner]string{
					stat.WinnerFirst:   "First",
					stat.WinnerSecond:  "Second",
					stat.WinnerUnclear: "Unclear",
				})[winner],
				WinnerConfidence: confidence,
				LOS:              status.LOS(),
				EloDiff:          status.EloDiff(0.95),
				CanCancel:        canCancel && data.Status.Kind == scheduler.ContestRunning,
				CSRFField:        csrf.TemplateField(req),
				FixedTime:        fixedTime,
				TimeControl:      timeControl,
				ScoreThreshold:   info.ScoreThreshold,
				First:            info.Players[0].Name,
				Second:           info.Players[1].Name,
				Total:            int(info.Match.Games),
			}, nil
		default:
			panic("unknown contest kind")
		}
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		switch req.FormValue("action") {
		case "cancel":
			if !canCancel {
				return nil, httputil.MakeError(http.StatusForbidden, "operation not permitted")
			}
			cfg.Scheduler.AbortContest(info.ID, "canceled by user "+bc.FullUser.Username)
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "contest canceled", "/contest/"+info.ID)
		default:
			return nil, httputil.MakeError(http.StatusBadRequest, "unknown action")
		}
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func contestPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, contestDataBuilder{}, "contest")
}

type contestPGNAttachImpl struct {
	log *slog.Logger
	cfg *Config
}

func (a *contestPGNAttachImpl) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := a.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
	log.Info("handle contest pgn request",
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
	)

	if req.Method != http.MethodGet {
		log.Warn("method not allowed")
		writeHTTPErr(log, w, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}

	contestID := req.PathValue("contestID")
	jobs, err := a.cfg.Scheduler.ListContestSucceededJobs(ctx, contestID)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoSuchContest) {
			writeHTTPErr(log, w, httputil.MakeError(http.StatusNotFound, "contest not found"))
			return
		}
		log.Warn("could not list finished jobs", slogx.Err(err))
		writeHTTPErr(log, w, httputil.MakeError(http.StatusInternalServerError, "internal server error"))
		return
	}

	w.Header().Set("Content-Type", "application/vnd.chess-pgn")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"contest_%v.pgn\"", contestID))
	first := true
	for _, job := range jobs {
		if job.PGN == nil {
			log.Error("pgn missing for succeeded job",
				slog.String("contest_id", contestID),
				slog.String("job_id", job.Job.ID),
			)
			continue
		}
		if !first {
			if _, err := io.WriteString(w, "\n"); err != nil {
				log.Info("could not write response", slogx.Err(err))
				return
			}
		}
		first = false
		if _, err := io.WriteString(w, *job.PGN); err != nil {
			log.Info("could not write response", slogx.Err(err))
			return
		}
	}
}

func contestPGNAttach(log *slog.Logger, cfg *Config) http.Handler {
	return &contestPGNAttachImpl{
		log: log,
		cfg: cfg,
	}
}
