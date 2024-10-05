package webui

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/stat"
	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/clock"
	"github.com/gorilla/csrf"
)

type contestDataBuilder struct{}

func (contestDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	req := bc.Req
	log := bc.Log

	type builtData struct {
		ID   string
		Name string

		CanCancel bool
		CSRFField template.HTML

		Kind           scheduler.ContestKind
		First          string
		Second         string
		Status         scheduler.ContestStatus
		Progress       *progressPartData
		Played         int64
		Total          int64
		FixedTime      *time.Duration
		TimeControl    *clock.Control
		ScoreThreshold int32
		OpeningBook    scheduler.OpeningBook

		FirstWin         int64
		Draw             int64
		SecondWin        int64
		Score            string
		LOS              float64
		Winner           stat.Winner
		WinnerConfidence string
		EloDiff          stat.EloDiff
	}

	info, data, err := cfg.Scheduler.GetContest(ctx, req.PathValue("contestID"))
	if err != nil {
		log.Info("could not get contest", slogx.Err(err))
		return nil, httputil.MakeError(http.StatusNotFound, "contest not found")
	}
	canCancel := bc.FullUser != nil && bc.FullUser.Perms.Get(userauth.PermRunContests)

	switch req.Method {
	case http.MethodGet:
		if info.Kind != scheduler.ContestMatch {
			panic("unknown contest kind")
		}
		ms := data.Match.Status()
		confidence, winner := ms.Winner(0.9, 0.95, 0.97, 0.99)
		confidenceStr := ""
		if confidence != 0.0 {
			confidenceStr = fmt.Sprintf("%02v", math.Round(confidence*100))
		}
		return &builtData{
			ID:   info.ID,
			Name: info.Name,

			CanCancel: canCancel && !data.Status.Kind.IsFinished(),
			CSRFField: csrf.TemplateField(req),

			Kind:           info.Kind,
			First:          info.Players[0].Name,
			Second:         info.Players[1].Name,
			Status:         data.Status,
			Progress:       buildProgressPartData(data.Match.Played(), info.Match.Games),
			Played:         data.Match.Played(),
			Total:          info.Match.Games,
			FixedTime:      info.FixedTime,
			TimeControl:    info.TimeControl,
			ScoreThreshold: info.ScoreThreshold,
			OpeningBook:    info.OpeningBook,

			FirstWin:         data.Match.FirstWin,
			Draw:             data.Match.Draw,
			SecondWin:        data.Match.SecondWin,
			Score:            ms.ScoreString(),
			LOS:              ms.LOS(),
			Winner:           winner,
			WinnerConfidence: confidenceStr,
			EloDiff:          ms.EloDiff(0.95),
		}, nil
	case http.MethodPost:
		if !bc.IsHTMX() {
			return nil, httputil.MakeError(http.StatusBadRequest, "must use htmx request")
		}
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
			return nil, bc.Redirect("/contest/" + info.ID)
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
