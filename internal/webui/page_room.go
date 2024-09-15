package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
)

const emptyFEN = "8/8/8/8/8/8/8/8 w - - 0 1"

type playerClockData struct {
	Msecs  int64
	Active bool
}

type cursorData struct {
	JSON         string
	ForceRefresh bool
}

type playerData struct {
	Color     string
	ColorText string
	ClockVar  template.JS
	Name      string
	Active    bool
	Clock     *playerClockData
	Score     string
	PV        string
	Depth     string
	Nodes     string
	NPS       string
}

func colorText(col chess.Color) string {
	if col == chess.ColorWhite {
		return "White"
	}
	return "Black"
}

func buildCursorData(log *slog.Logger, cursor maybe.Maybe[delta.RoomCursor], forceRefresh bool) cursorData {
	jsonData := "{}"
	if cursor.IsSome() {
		jsonBytes, err := json.Marshal(struct {
			C delta.RoomCursor `json:"c"`
		}{C: cursor.Get()})
		if err != nil {
			log.Error("could not marshal cursor", slogx.Err(err))
		} else {
			jsonData = string(jsonBytes)
		}
	}
	return cursorData{
		JSON:         jsonData,
		ForceRefresh: forceRefresh,
	}
}

func buildPlayerData(col chess.Color, state *delta.JobState) playerData {
	playerName := ""
	if state != nil && state.Info != nil {
		playerName = state.Info.PlayerInfo(col)
	}
	data := playerData{
		Color:     col.LongString(),
		ColorText: colorText(col),
		ClockVar:  template.JS(col.LongString() + "Clock"),
		Name:      playerName,
		Active:    false,
		Clock:     nil,
		Score:     "-",
		PV:        "",
		Depth:     "-",
		Nodes:     "-",
		NPS:       "-",
	}
	var player *delta.Player
	if state != nil {
		player = state.Player(col)
	}
	if player == nil {
		return data
	}
	data.Active = player.Active
	if c, ok := player.ClockFrom(delta.NowTimestamp()).TryGet(); ok {
		data.Clock = &playerClockData{
			Active: player.Active,
			Msecs:  c.Milliseconds(),
		}
	}
	if s, ok := player.Score.TryGet(); ok {
		data.Score = s.String()
	}
	data.PV = player.PVS
	if player.Depth != 0 {
		data.Depth = strconv.FormatInt(int64(player.Depth), 10)
	}
	if player.Nodes != 0 {
		data.Nodes = strconv.FormatInt(player.Nodes, 10)
	}
	if player.NPS != 0 {
		data.NPS = strconv.FormatInt(player.NPS, 10)
	}
	return data
}

type roomDataBuilder struct{}

func (roomDataBuilder) Build(ctx context.Context, log *slog.Logger, cfg *Config, req *http.Request) (any, error) {
	_ = ctx
	_ = log

	type data struct {
		ID     string
		Name   string
		Cursor cursorData
		FEN    string
		White  playerData
		Black  playerData
	}

	roomID := req.PathValue("roomID")
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

	dat := &data{
		ID:     info.ID,
		Name:   info.Name,
		Cursor: buildCursorData(log, maybe.Some(state.Cursor()), false),
		FEN:    emptyFEN,
		White:  buildPlayerData(chess.ColorWhite, state.State),
		Black:  buildPlayerData(chess.ColorBlack, state.State),
	}
	if state.State != nil && state.State.Position.Board != nil {
		dat.FEN = state.State.Position.Board.FEN()
	}

	return dat, nil
}

func roomPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, templ, roomDataBuilder{}, "room", "player", "fen", "cursor")
}

type roomPGNPageImpl struct {
	log *slog.Logger
	cfg *Config
}

func (p *roomPGNPageImpl) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := p.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
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
	game, err := p.cfg.Keeper.RoomGameExt(roomID)
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

	w.Header().Set("Content-Type", "text/plain")
	if _, err := io.WriteString(w, pgn); err != nil {
		log.Info("could not write response", slogx.Err(err))
	}
}

func roomPGNAttach(log *slog.Logger, cfg *Config) http.Handler {
	return &roomPGNPageImpl{
		log: log,
		cfg: cfg,
	}
}
