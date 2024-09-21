package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/httputil"
)

type profileDataBuilder struct{}

func (profileDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	if bc.UserInfo == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	return nil, httputil.MakeRedirectError(http.StatusSeeOther, "redirecting to profile", "/user/"+bc.UserInfo.Username)
}

func profilePage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoShowAuth: true}, templ, profileDataBuilder{}, "")
}
