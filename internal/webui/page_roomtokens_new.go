package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
)

type roomtokensNewData struct {
	Token string
}

type roomtokensNewDataBuilder struct{}

func (roomtokensNewDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	if bc.FullUser == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	if !bc.FullUser.Perms.Get(userauth.PermHostRooms) {
		return nil, httputil.MakeError(http.StatusForbidden, "room tokens not allowed")
	}

	switch req.Method {
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		tok, err := cfg.UserManager.GenerateRoomToken(ctx, req.FormValue("token-label"), bc.FullUser)
		if err != nil {
			log.Warn("could not generate room token", slogx.Err(err))
			return nil, fmt.Errorf("generate room token: %w", err)
		}
		return &roomtokensNewData{Token: tok}, nil
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func roomtokensNewPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, roomtokensNewDataBuilder{}, "roomtokens_new")
}
