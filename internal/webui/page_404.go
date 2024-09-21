package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
)

type e404DataBuilder struct{}

func (e404DataBuilder) Build(context.Context, builderCtx) (any, error) {
	return nil, httputil.MakeError(http.StatusNotFound, "page not found")
}

func e404Page(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoUserInfo: true}, templ, e404DataBuilder{}, "")
}
