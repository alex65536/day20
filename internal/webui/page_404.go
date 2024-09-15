package webui

import (
	"context"
	"log/slog"
	"net/http"

	httputil "github.com/alex65536/day20/internal/util/http"
)

type e404DataBuilder struct{}

func (e404DataBuilder) Build(context.Context, *slog.Logger, *Config, *http.Request) (any, error) {
	return nil, httputil.MakeError(http.StatusNotFound, "page not found")
}

func e404Page(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, templ, e404DataBuilder{}, "")
}
