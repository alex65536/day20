package webui

import (
	"cmp"
	"context"
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

type roomtokensDataItem struct {
	CreatedAt timeutil.UTCTime
	Hash      string
	Name      string
}

type roomtokensData struct {
	CSRFField template.HTML
	Tokens    []roomtokensDataItem
}

type roomtokensDataBuilder struct{}

func (roomtokensDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	if bc.FullUser == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	if !bc.FullUser.Perms.Get(userauth.PermHostRooms) {
		return nil, httputil.MakeError(http.StatusForbidden, "room tokens not allowed")
	}

	var tokens []roomtokensDataItem
	for _, t := range bc.FullUser.RoomTokens {
		tokens = append(tokens, roomtokensDataItem{
			CreatedAt: t.CreatedAt,
			Hash:      t.Hash,
			Name:      t.Name,
		})
	}
	slices.SortFunc(tokens, func(a, b roomtokensDataItem) int {
		return cmp.Or(
			b.CreatedAt.Compare(a.CreatedAt),
			cmp.Compare(a.Hash, b.Hash),
		)
	})

	data := &roomtokensData{
		CSRFField: csrf.TemplateField(req),
		Tokens:    tokens,
	}

	switch req.Method {
	case http.MethodGet:
		return data, nil
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		switch req.FormValue("action") {
		case "delete":
			if err := cfg.UserManager.DeleteRoomToken(ctx, req.FormValue("hash"), bc.FullUser.ID); err != nil {
				log.Error("could not delete room token", slogx.Err(err))
				return nil, fmt.Errorf("delete room token: %w", err)
			}
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "room token deleted", "/roomtokens")
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
