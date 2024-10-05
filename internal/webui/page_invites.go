package webui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/csrf"
)

type invitesDataBuilder struct{}

func (invitesDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log
	now := time.Now()

	type item struct {
		CreatedAt timeutil.UTCTime
		Label     string
		Link      string
		Perms     *permsData
		ExpiresAt *humanTimePartData
		Hash      string
	}

	type data struct {
		CSRFField template.HTML
		Perms     *permsData
		Invites   []item
	}

	if bc.FullUser == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	if !bc.FullUser.Perms.Get(userauth.PermInvite) {
		return nil, httputil.MakeError(http.StatusForbidden, "inviting not allowed")
	}

	switch req.Method {
	case http.MethodGet:
		invites := make([]item, 0, len(bc.FullUser.InviteLinks))
		for _, l := range bc.FullUser.InviteLinks {
			if l.ExpiresAt.UTC().Before(now) {
				continue
			}
			invites = append(invites, item{
				CreatedAt: l.CreatedAt,
				Label:     l.Label,
				Link:      cfg.UserManager.InviteLinkURL(l),
				Perms:     buildPermsData(l.Perms),
				ExpiresAt: buildHumanTimePartData(now, l.ExpiresAt.UTC()),
				Hash:      l.Hash,
			})
		}
		slices.SortFunc(invites, func(a, b item) int {
			return cmp.Or(
				b.CreatedAt.Compare(a.CreatedAt),
				cmp.Compare(a.Hash, b.Hash),
			)
		})

		return &data{
			CSRFField: csrf.TemplateField(req),
			Perms:     buildPermsData(bc.FullUser.Perms),
			Invites:   invites,
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
		case "invite":
			label := req.FormValue("invite-label")
			if label == "" {
				return &errorsPartData{
					Errors: []string{"no link label"},
				}, nil
			}
			var perms userauth.Perms
			for p := range userauth.PermMax {
				if req.FormValue("invite-perm-"+p.String()) == "true" {
					*perms.GetMut(p) = true
				}
			}
			_, err := cfg.UserManager.GenerateInviteLink(ctx, label, bc.FullUser, perms)
			if err != nil {
				var verifyErr *userauth.ErrorInviteLinkVerify
				if errors.As(err, &verifyErr) {
					return nil, httputil.MakeError(http.StatusForbidden, verifyErr.Unwrap().Error())
				}
				log.Warn("could not create invite link", slogx.Err(err))
				return nil, fmt.Errorf("create invite link: %w", err)
			}
			return nil, bc.Redirect("/invites")
		case "delete":
			if err := cfg.UserManager.DeleteInviteLink(ctx, req.FormValue("hash"), bc.FullUser.ID); err != nil {
				log.Warn("could not delete invite link", slogx.Err(err))
				return nil, fmt.Errorf("delete invite link: %w", err)
			}
			return nil, bc.Redirect("/invites")
		default:
			return nil, httputil.MakeError(http.StatusBadRequest, "unknown action")
		}
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func invitesPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{
		FullUser: true,
		GetUserOptions: maybe.Some(userauth.GetUserOptions{
			WithInviteLinks: true,
		}),
	}, templ, invitesDataBuilder{}, "invites")
}
