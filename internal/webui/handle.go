package webui

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/id"
	websockutil "github.com/alex65536/day20/internal/util/websocket"
)

type Config struct {
	Keeper   *roomkeeper.Keeper
	ServerID string
	prefix   string
	opts     *Options
}

type Options struct {
	WebSocket         websockutil.Options
	ReadCursorTimeout time.Duration
	RoomRPSLimit      float64
	RoomRPSBurst      int
}

func (o *Options) FillDefaults() {
	o.WebSocket.FillDefaults()
	if o.ReadCursorTimeout == 0 {
		o.ReadCursorTimeout = 30 * time.Second
	}
	if o.RoomRPSLimit == 0.0 {
		o.RoomRPSLimit = 3
	}
	if o.RoomRPSBurst == 0 {
		o.RoomRPSBurst = 5
	}
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func Handle(log *slog.Logger, mux *http.ServeMux, prefix string, cfg Config, o Options) {
	o.FillDefaults()

	if cfg.ServerID == "" {
		cfg.ServerID = id.ID()
	}
	cfg.prefix = prefix
	cfg.opts = &o
	b := middlewareBuilder{
		Log:    log,
		Prefix: prefix,
	}
	templ := newTemplator(&cfg)

	mux.Handle(prefix+"/img/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/css/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/js/", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/favicon.ico", b.WrapStatic(http.FileServerFS(staticData)))
	mux.Handle(prefix+"/{$}", b.WrapPage(must(mainPage(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}", b.WrapPage(must(roomPage(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}/ws", b.WrapWebSocket(must(roomWebSocket(log, &cfg, templ))))
	mux.Handle(prefix+"/room/{roomID}/pgn", b.WrapAttach(roomPGNAttach(log, &cfg)))
	mux.Handle(prefix+"/", b.WrapPage(must(e404Page(log, &cfg, templ))))
}
