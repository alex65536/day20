package webui

import (
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
)

type middlewareBuilder struct {
	Log         *slog.Logger
	Prefix      string
	CSRFProtect func(http.Handler) http.Handler
}

type middleware struct {
	b    *middlewareBuilder
	h    http.Handler
	kind string
}

func (m *middleware) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = httputil.WrapRequest(req)
	m.b.Log.Info("handle request",
		slog.String("rid", httputil.ExtractReqID(req.Context())),
		slog.String("uri", req.RequestURI),
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
		slog.String("kind", m.kind),
	)
	switch m.kind {
	case "page":
	case "attach":
	case "websocket":
	case "static":
		w.Header().Set("Cache-Control", "max-age=7200, public")
	default:
		panic("must not happen")
	}
	m.h.ServeHTTP(w, req)
}

func (b *middlewareBuilder) WrapPage(h http.Handler) http.Handler {
	return &middleware{b: b, h: b.CSRFProtect(h), kind: "page"}
}

func (b *middlewareBuilder) WrapAttach(h http.Handler) http.Handler {
	return &middleware{b: b, h: h, kind: "attach"}
}

func (b *middlewareBuilder) WrapStatic(h http.Handler) http.Handler {
	return &middleware{b: b, h: h, kind: "static"}
}

func (b *middlewareBuilder) WrapWebSocket(h http.Handler) http.Handler {
	return &middleware{b: b, h: h, kind: "websocket"}
}
