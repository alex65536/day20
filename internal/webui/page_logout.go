package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
)

type logoutDataBuilder struct{}

func (logoutDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	bc.ResetSession(nil)
	return nil, httputil.MakeRedirectError(http.StatusSeeOther, "logged out", "/")
}

func logoutPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoUserInfo: true}, templ, logoutDataBuilder{}, "")
}
