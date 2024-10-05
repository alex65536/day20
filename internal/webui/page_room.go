package webui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
)

type roomDataBuilder struct{}

func (roomDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config
	log := bc.Log

	type data struct {
		ID      string
		Name    string
		Cursor  *cursorPartData
		FEN     *fenPartData
		White   *playerPartData
		Black   *playerPartData
		Buttons *roomButtonsPartData
	}

	roomID := bc.Req.PathValue("roomID")
	info, err := cfg.Keeper.RoomInfo(roomID)
	if err != nil {
		if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
			return nil, httputil.MakeError(http.StatusNotFound, "room not found")
		}
		return nil, fmt.Errorf("get room info: %w", err)
	}
	state := delta.NewRoomState()
	delta, _, err := cfg.Keeper.RoomStateDelta(roomID, delta.RoomCursor{})
	if err != nil {
		if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
			return nil, httputil.MakeError(http.StatusNotFound, "room not found")
		}
		return nil, fmt.Errorf("compute delta: %w", err)
	}
	if err := state.ApplyDelta(delta); err != nil {
		return nil, fmt.Errorf("apply delta: %w", err)
	}
	var board *chess.Board
	if state.State != nil {
		board = state.State.Position.Board
	}

	return &data{
		ID:     info.ID,
		Name:   info.Name,
		Cursor: buildCursorPartData(log, maybe.Some(state.Cursor()), false),
		FEN:    buildFENPartData(board),
		White:  buildPlayerPartData(chess.ColorWhite, state.State),
		Black:  buildPlayerPartData(chess.ColorBlack, state.State),
		Buttons: &roomButtonsPartData{
			RoomID: roomID,
			Active: state.JobID != "",
		},
	}, nil
}

func roomPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, roomDataBuilder{}, "room")
}

type roomPGNAttachImpl struct {
	log *slog.Logger
	cfg *Config
}

func (a *roomPGNAttachImpl) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := a.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
	log.Info("handle room pgn request",
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
	)

	if req.Method != http.MethodGet {
		log.Warn("method not allowed")
		writeHTTPErr(log, w, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}

	roomID := req.PathValue("roomID")
	game, err := a.cfg.Keeper.RoomGameExt(roomID)
	if err != nil {
		if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
			writeHTTPErr(log, w, httputil.MakeError(http.StatusNotFound, "room not found"))
			return
		}
		if roomapi.MatchesError(err, roomapi.ErrNoJobRunning) {
			writeHTTPErr(log, w, httputil.MakeError(http.StatusNotFound, "job not found"))
			return
		}
		if errors.Is(err, roomkeeper.ErrGameNotReady) {
			writeHTTPErr(log, w, httputil.MakeError(http.StatusNotFound, "game not ready"))
			return
		}
		log.Error("could not build game", slogx.Err(err))
		writeHTTPErr(log, w, httputil.MakeError(http.StatusInternalServerError, "error building game"))
		return
	}
	pgn, err := game.PGN()
	if err != nil {
		log.Error("could not convert game", slogx.Err(err))
		writeHTTPErr(log, w, httputil.MakeError(http.StatusInternalServerError, "error converting game"))
		return
	}

	w.Header().Set("Content-Type", "application/vnd.chess-pgn")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"room_%v.pgn\"", roomID))
	if _, err := io.WriteString(w, pgn); err != nil {
		log.Info("could not write response", slogx.Err(err))
	}
}

func roomPGNAttach(log *slog.Logger, cfg *Config) http.Handler {
	return &roomPGNAttachImpl{
		log: log,
		cfg: cfg,
	}
}
