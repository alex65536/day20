package webui

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/day20/internal/util/slogx"
	websockutil "github.com/alex65536/day20/internal/util/websocket"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

type roomWebSocketSession struct {
	req    *http.Request
	log    *slog.Logger
	cfg    *Config
	templ  *templator
	s      *websockutil.Session
	recvCh chan []byte
}

func (s *roomWebSocketSession) recvCursor() (delta.RoomCursor, error) {
	select {
	case msg := <-s.recvCh:
		var data struct {
			C delta.RoomCursor `json:"c"`
		}
		if err := json.Unmarshal(msg, &data); err != nil {
			return delta.RoomCursor{}, fmt.Errorf("unmarshal cursor: %w", err)
		}
		return data.C, nil
	case <-time.After(s.cfg.opts.ReadCursorTimeout):
		return delta.RoomCursor{}, fmt.Errorf("cursor read timed out")
	case <-s.s.Done():
		return delta.RoomCursor{}, io.EOF
	}
}

func (s *roomWebSocketSession) shutdownWithPageRefresh() {
	body, err := s.templ.RenderFragment("cursor-ajax", "cursor", struct {
		cursorData
		AJAXAttrs template.HTML
	}{
		cursorData: buildCursorData(s.log, maybe.None[delta.RoomCursor](), true),
		AJAXAttrs:  template.HTML(`hx-swap-oob="outerHTML"`),
	})
	if err != nil {
		s.log.Error("could not render cursor", slogx.Err(err))
		s.s.Shutdown()
		return
	}
	if err := s.s.WriteMsg(websocket.TextMessage, body); err != nil {
		s.log.Info("could not write message", slogx.Err(err))
		s.s.Close()
		return
	}
	s.s.Shutdown()
}

func (s *roomWebSocketSession) renderAndSend(key, fragment string, cursor delta.RoomCursor, data any) bool {
	fragmentBody, err := s.templ.RenderFragment(key, fragment, data)
	if err != nil {
		s.log.Error("could not render fragment", slogx.Err(err))
		s.s.Shutdown()
		return false
	}
	cursorBody, err := s.templ.RenderFragment("cursor-ajax", "cursor", struct {
		cursorData
		AJAXAttrs template.HTML
	}{
		cursorData: buildCursorData(s.log, maybe.Some(cursor), false),
		AJAXAttrs:  template.HTML(`hx-swap-oob="outerHTML"`),
	})
	if err != nil {
		s.log.Error("could not render cursor", slogx.Err(err))
		s.s.Shutdown()
		return false
	}
	body := slices.Concat(fragmentBody, []byte{'\n'}, cursorBody)
	if err := s.s.WriteMsg(websocket.TextMessage, body); err != nil {
		s.log.Info("could not write message", slogx.Err(err))
		return false
	}
	return true
}

func (s *roomWebSocketSession) Do() {
	defer s.s.Close()

	log := s.log
	roomID := s.req.PathValue("roomID")
	clientCursor, err := s.recvCursor()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return
		}
		log.Warn("error getting cursor", slogx.Err(err))
		s.s.Shutdown()
		return
	}

	sub, unsub, ok := s.cfg.Keeper.Subscribe(roomID)
	if !ok {
		s.shutdownWithPageRefresh()
		return
	}
	defer unsub()

	limit := rate.NewLimiter(rate.Limit(s.cfg.opts.RoomRPSLimit), s.cfg.opts.RoomRPSBurst)
	state := delta.NewRoomState()
	for {
		ourDelta, _, err := s.cfg.Keeper.RoomStateDelta(roomID, state.Cursor())
		if err != nil {
			if roomapi.MatchesError(err, roomapi.ErrNoSuchRoom) {
				s.shutdownWithPageRefresh()
				return
			}
			log.Warn("could not get room state delta", slogx.Err(err))
			s.s.Shutdown()
			return
		}
		if err := state.ApplyDelta(ourDelta); err != nil {
			log.Warn("could not apply room state delta", slogx.Err(err))
			s.s.Shutdown()
			return
		}

		oldClientCursor := clientCursor
		clientCursor = state.Cursor()

		if oldClientCursor.JobID != clientCursor.JobID ||
			oldClientCursor.State.Position != clientCursor.State.Position {
			data := struct {
				FEN       string
				AJAXAttrs template.HTML
			}{
				FEN:       emptyFEN,
				AJAXAttrs: template.HTML(`hx-swap-oob="outerHTML"`),
			}
			if state.State != nil && state.State.Position.Board != nil {
				data.FEN = state.State.Position.Board.FEN()
			}
			if !s.renderAndSend("fen-ajax", "fen", clientCursor, data) {
				return
			}
		}

		for col := range chess.ColorMax {
			if oldClientCursor.JobID == clientCursor.JobID &&
				oldClientCursor.State.Player(col) == clientCursor.State.Player(col) &&
				oldClientCursor.State.HasInfo == clientCursor.State.HasInfo {
				continue
			}
			data := struct {
				playerData
				AJAXAttrs template.HTML
			}{
				playerData: buildPlayerData(col, state.State),
				AJAXAttrs:  template.HTML(`hx-swap-oob="outerHTML"`),
			}
			if !s.renderAndSend("player-ajax", "player", clientCursor, data) {
				return
			}
		}

		if err := limit.Wait(s.req.Context()); err != nil {
			return
		}
		<-sub
	}
}

type roomWebSocketImpl struct {
	log     *slog.Logger
	cfg     *Config
	templ   *templator
	factory *websockutil.SessionFactory
}

func roomWebSocket(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	if err := templ.Add("fen-ajax", "fen", "ajax"); err != nil {
		return nil, fmt.Errorf("add template: %w", err)
	}
	if err := templ.Add("player-ajax", "player", "ajax"); err != nil {
		return nil, fmt.Errorf("add template: %w", err)
	}
	if err := templ.Add("cursor-ajax", "cursor", "ajax"); err != nil {
		return nil, fmt.Errorf("add template: %w", err)
	}
	return &roomWebSocketImpl{
		log:     log,
		cfg:     cfg,
		templ:   templ,
		factory: websockutil.NewSessionFactory(cfg.opts.WebSocket),
	}, nil
}

func (s *roomWebSocketImpl) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := s.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
	log.Info("handle room websocket", slog.String("addr", req.RemoteAddr))
	recvCh := make(chan []byte, 1)
	sendCh := recvCh

	session, err := s.factory.NewSession(w, req, log, func(msg []byte) error {
		if sendCh == nil {
			log.Info("unexpected message from socket")
			return nil
		}
		sendCh <- msg
		close(sendCh)
		sendCh = nil
		return nil
	})
	if err != nil {
		return
	}

	roomSession := &roomWebSocketSession{
		req:    req,
		log:    log,
		cfg:    s.cfg,
		templ:  s.templ,
		s:      session,
		recvCh: recvCh,
	}
	roomSession.Do()
}
