package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/websockutil"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

type roomWebSocketSession struct {
	req    *http.Request
	log    *slog.Logger
	cfg    *Config
	tmpl   *template.Template
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
	var b bytes.Buffer
	cursorData := buildCursorPartData(s.log, maybe.None[delta.RoomCursor](), true)
	cursorData.AJAXAttrs = template.HTMLAttr(`hx-swap-oob="outerHTML"`)
	if err := s.tmpl.ExecuteTemplate(&b, "part/cursor", cursorData); err != nil {
		s.log.Error("could not render cursor", slogx.Err(err))
		s.s.Shutdown()
		return
	}
	if err := s.s.WriteMsg(websocket.TextMessage, b.Bytes()); err != nil {
		s.log.Info("could not write message", slogx.Err(err))
		s.s.Close()
		return
	}
	s.s.Shutdown()
}

func (s *roomWebSocketSession) renderAndSend(fragment string, cursor delta.RoomCursor, data any) bool {
	var b bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&b, fragment, data); err != nil {
		s.log.Error("could not render fragment", slogx.Err(err))
		s.s.Shutdown()
		return false
	}
	_ = b.WriteByte('\n')
	cursorData := buildCursorPartData(s.log, maybe.Some(cursor), false)
	cursorData.AJAXAttrs = template.HTMLAttr(`hx-swap-oob="outerHTML"`)
	if err := s.tmpl.ExecuteTemplate(&b, "part/cursor", cursorData); err != nil {
		s.log.Error("could not render cursor", slogx.Err(err))
		s.s.Shutdown()
		return false
	}
	if err := s.s.WriteMsg(websocket.TextMessage, b.Bytes()); err != nil {
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

		if oldClientCursor.JobID != clientCursor.JobID {
			roomButtonsData := &roomButtonsPartData{
				RoomID:    roomID,
				Active:    clientCursor.JobID != "",
				AJAXAttrs: template.HTMLAttr(`hx-swap-oob="outerHTML"`),
			}
			if !s.renderAndSend("part/room_buttons", clientCursor, roomButtonsData) {
				return
			}
		}

		if oldClientCursor.JobID != clientCursor.JobID ||
			oldClientCursor.State.Position != clientCursor.State.Position {
			var board *chess.Board
			if state.State != nil {
				board = state.State.Position.Board
			}
			fenData := buildFENPartData(board)
			fenData.AJAXAttrs = template.HTMLAttr(`hx-swap-oob="outerHTML"`)
			if !s.renderAndSend("part/fen", clientCursor, fenData) {
				return
			}
		}

		for col := range chess.ColorMax {
			if oldClientCursor.JobID == clientCursor.JobID &&
				oldClientCursor.State.Player(col) == clientCursor.State.Player(col) &&
				oldClientCursor.State.HasInfo == clientCursor.State.HasInfo {
				continue
			}
			playerData := buildPlayerPartData(col, state.State)
			playerData.AJAXAttrs = template.HTMLAttr(`hx-swap-oob="outerHTML"`)
			if !s.renderAndSend("part/player", clientCursor, playerData) {
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
	tmpl    *template.Template
	factory *websockutil.SessionFactory
}

func roomWebSocket(log *slog.Logger, cfg *Config, templator *templator) (http.Handler, error) {
	tmpl, err := templator.Get("")
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	return &roomWebSocketImpl{
		log:     log,
		cfg:     cfg,
		tmpl:    tmpl,
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
		tmpl:   s.tmpl,
		s:      session,
		recvCh: recvCh,
	}
	roomSession.Do()
}
