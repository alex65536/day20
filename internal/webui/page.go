package webui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
)

type dataBuilder interface {
	Build(ctx context.Context, log *slog.Logger, cfg *Config, req *http.Request) (any, error)
}

type page struct {
	name  string
	cfg   *Config
	log   *slog.Logger
	b     dataBuilder
	templ *templator
}

func (p *page) renderError(log *slog.Logger, w http.ResponseWriter, httpErr *httputil.Error) {
	log.Info("send http status error",
		slog.Int("code", httpErr.Code()),
		slog.String("msg", httpErr.Message()),
	)
	page, err := p.templ.Render("error", struct {
		Code    int
		Message string
	}{
		Code:    httpErr.Code(),
		Message: httpErr.Message(),
	})
	if err != nil {
		log.Error("error rendering page", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("render page"))
		return
	}
	w.Header().Set("Context-Type", "text/html")
	httpErr.ApplyHeaders(w)
	w.WriteHeader(httpErr.Code())
	if _, err := w.Write(page); err != nil {
		log.Error("error writing page data", slogx.Err(err))
		return
	}
}

func (p *page) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	log := p.log.With(slog.String("rid", httputil.ExtractReqID(ctx)))
	log.Info("handle page request",
		slog.String("method", req.Method),
		slog.String("addr", req.RemoteAddr),
	)

	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		log.Warn("method not allowed")
		writeHTTPErr(log, w, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}

	data, err := p.b.Build(ctx, log, p.cfg, req)
	if err != nil {
		if httpErr := (*httputil.Error)(nil); errors.As(err, &httpErr) {
			p.renderError(log, w, httpErr)
			return
		}
		log.Error("error building page data", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("build page"))
		return
	}

	page, err := p.templ.Render(p.name, data)
	if err != nil {
		log.Error("error rendering page", slogx.Err(err))
		writeHTTPErr(log, w, fmt.Errorf("render page"))
		return
	}

	w.Header().Set("Context-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(page); err != nil {
		log.Error("error writing page data", slogx.Err(err))
		return
	}
}

func newPage(log *slog.Logger, cfg *Config, templ *templator, builder dataBuilder, name string, deps ...string) (http.Handler, error) {
	if name != "" {
		if err := templ.Add(name, append([]string{name}, deps...)...); err != nil {
			return nil, fmt.Errorf("template %v: %w", name, err)
		}
	}
	if !templ.Has("error") {
		if err := templ.Add("error", "error"); err != nil {
			return nil, fmt.Errorf("template error: %w", err)
		}
	}
	return &page{
		name:  name,
		cfg:   cfg,
		log:   log.With(slog.String("page", name)),
		b:     builder,
		templ: templ,
	}, nil
}
