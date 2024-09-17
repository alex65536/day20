package websockutil

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/gorilla/websocket"
)

type msg struct {
	kind int
	data []byte
}

type ReceiverFunc func(msg []byte) error

type Session struct {
	conn *websocket.Conn
	log  *slog.Logger
	o    *Options
	recv ReceiverFunc

	writeCh chan msg
	closeCh chan struct{}
	wg      sync.WaitGroup

	ctx    context.Context
	cancel func()
	closed atomic.Bool
}

type SessionFactory struct {
	o        Options
	upgrader websocket.Upgrader
}

func NewSessionFactory(o Options) *SessionFactory {
	o.FillDefaults()
	return &SessionFactory{
		o:        o,
		upgrader: o.Upgrader(),
	}
}

func (f *SessionFactory) NewSession(
	w http.ResponseWriter,
	req *http.Request,
	log *slog.Logger,
	recv ReceiverFunc,
) (*Session, error) {
	conn, err := f.upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Warn("could not upgrade websocket", slogx.Err(err))
		return nil, fmt.Errorf("upgrade: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		conn:    conn,
		log:     log,
		o:       &f.o,
		recv:    recv,
		writeCh: make(chan msg),
		closeCh: make(chan struct{}, 1),
		ctx:     ctx,
		cancel:  cancel,
	}
	s.closed.Store(false)
	s.wg.Add(2)
	go s.readLoop()
	go s.WriteLoop()
	return s, nil
}

func (s *Session) Done() <-chan struct{} {
	return s.ctx.Done()
}

func (s *Session) Close() {
	if s.closed.Swap(true) {
		<-s.ctx.Done()
		return
	}
	s.cancel()
	if err := s.conn.Close(); err != nil {
		s.log.Info("could not close websocket", slogx.Err(err))
	}
	s.wg.Wait()
}

func (s *Session) readLoop() {
	defer s.wg.Done()
	defer s.Close()
	for {
		s.conn.SetReadLimit(s.o.ReadMsgLimit)
		_ = s.conn.SetReadDeadline(time.Now().Add(s.o.PingTimeout))
		s.conn.SetPongHandler(func(string) error {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.o.PingTimeout))
			return nil
		})
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			if !websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.log.Info("could not read from websocket", slogx.Err(err))
			}
			return
		}
		if err := s.recv(msg); err != nil {
			s.log.Info("could not receive message", slogx.Err(err))
			s.Shutdown()
			return
		}
	}
}

func (s *Session) WriteLoop() {
	defer s.wg.Done()
	defer s.Close()
	ticker := time.NewTicker(s.o.PingInterval)
	defer ticker.Stop()
	for {
		var cur msg
		shutdown := false
		select {
		case <-s.closeCh:
			cur = msg{kind: websocket.CloseMessage, data: []byte{}}
			shutdown = true
		case cur = <-s.writeCh:
		case <-ticker.C:
			cur = msg{kind: websocket.PingMessage, data: []byte{}}
		case <-s.ctx.Done():
			return
		}
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.o.WriteDeadline))
		if err := s.conn.WriteMessage(cur.kind, cur.data); err != nil {
			s.log.Info("could not send close message", slogx.Err(err))
			return
		}
		if shutdown {
			return
		}
	}
}

func (s *Session) Shutdown() {
	select {
	case s.closeCh <- struct{}{}:
	default:
	}
	<-s.ctx.Done()
}

func (s *Session) WriteMsg(kind int, data []byte) error {
	select {
	case s.writeCh <- msg{kind: kind, data: data}:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
