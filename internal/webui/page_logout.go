package webui

import (
	"context"
	"log/slog"
	"net/http"
)

type logoutDataBuilder struct{}

func (logoutDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	bc.ResetSession(nil)
	return nil, bc.Redirect("/")
}

func logoutPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoUserInfo: true}, templ, logoutDataBuilder{}, "")
}
