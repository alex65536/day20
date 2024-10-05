package webui

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"slices"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/csrf"
)

type roomtokensDataBuilder struct{}

func (roomtokensDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	type item struct {
		CreatedAt timeutil.UTCTime
		FullHash  string
		ShortHash string
		Label     string
	}

	type data struct {
		CSRFField template.HTML
		Tokens    []item
	}

	if bc.FullUser == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	if !bc.FullUser.Perms.Get(userauth.PermHostRooms) {
		return nil, httputil.MakeError(http.StatusForbidden, "room tokens not allowed")
	}

	switch req.Method {
	case http.MethodGet:
		var tokens []item
		for _, t := range bc.FullUser.RoomTokens {
			hash := "<invalid>"
			rawHash, err := base64.RawURLEncoding.DecodeString(t.Hash)
			if err == nil && len(rawHash) >= 8 {
				hash = hex.EncodeToString(rawHash[len(rawHash)-8:])
			}
			tokens = append(tokens, item{
				CreatedAt: t.CreatedAt,
				FullHash:  t.Hash,
				ShortHash: hash,
				Label:     t.Label,
			})
		}
		slices.SortFunc(tokens, func(a, b item) int {
			return cmp.Or(
				b.CreatedAt.Compare(a.CreatedAt),
				cmp.Compare(a.FullHash, b.FullHash),
			)
		})
		return &data{
			CSRFField: csrf.TemplateField(req),
			Tokens:    tokens,
		}, nil
	case http.MethodPost:
		if !bc.IsHTMX() {
			return nil, httputil.MakeError(http.StatusBadRequest, "must use htmx request")
		}
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		switch req.FormValue("action") {
		case "delete":
			if err := cfg.UserManager.DeleteRoomToken(ctx, req.FormValue("hash"), bc.FullUser.ID); err != nil {
				log.Warn("could not delete room token", slogx.Err(err))
				return nil, fmt.Errorf("delete room token: %w", err)
			}
			return nil, bc.Redirect("/roomtokens")
		default:
			return nil, httputil.MakeError(http.StatusBadRequest, "unknown action")
		}
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func roomtokensPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{
		FullUser: true,
		GetUserOptions: maybe.Some(userauth.GetUserOptions{
			WithRoomTokens: true,
		}),
	}, templ, roomtokensDataBuilder{}, "roomtokens")
}
