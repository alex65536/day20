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
	"strings"
	"time"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/csrf"
)

type invitesDataPerm struct {
	Name  string
	Field string
}

type invitesDataItem struct {
	CreatedAt timeutil.UTCTime
	Name      string
	Link      string
	Perms     string
	ExpiresAt string
	Hash      string
}

type invitesData struct {
	CSRFField template.HTML
	Perms     []invitesDataPerm
	Invites   []invitesDataItem
}

type invitesDataBuilder struct{}

func (invitesDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	if bc.FullUser == nil {
		return nil, httputil.MakeError(http.StatusForbidden, "not logged in")
	}
	if !bc.FullUser.Perms.Get(userauth.PermInvite) {
		return nil, httputil.MakeError(http.StatusForbidden, "inviting not allowed")
	}

	var perms []invitesDataPerm
	for p := range userauth.PermMax {
		if bc.FullUser.Perms.Get(p) {
			perms = append(perms, invitesDataPerm{
				Name:  p.PrettyString(),
				Field: "invite-perm-" + p.String(),
			})
		}
	}

	var invites []invitesDataItem
	for _, l := range bc.FullUser.InviteLinks {
		perms := []string{}
		for p := range userauth.PermMax {
			if l.Perms.Get(p) {
				perms = append(perms, p.String())
			}
		}
		invites = append(invites, invitesDataItem{
			CreatedAt: l.CreatedAt,
			Name:      l.Name,
			Link:      cfg.UserManager.InviteLinkURL(l),
			Perms:     strings.Join(perms, ", "),
			ExpiresAt: l.ExpiresAt.Local().Format(time.RFC1123),
			Hash:      l.Hash,
		})
	}
	slices.SortFunc(invites, func(a, b invitesDataItem) int {
		return cmp.Or(
			b.CreatedAt.Compare(a.CreatedAt),
			cmp.Compare(a.Hash, b.Hash),
		)
	})

	data := &invitesData{
		CSRFField: csrf.TemplateField(req),
		Perms:     perms,
		Invites:   invites,
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
			if err := cfg.UserManager.DeleteInviteLink(ctx, req.FormValue("hash")); err != nil {
				log.Error("could not delete invite link", slogx.Err(err))
				return nil, fmt.Errorf("delete invite link: %w", err)
			}
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "invite link deleted", "/invites")
		case "invite":
			var perms userauth.Perms
			for p := range userauth.PermMax {
				if req.FormValue("invite-perm-"+p.String()) == "true" {
					*perms.GetMut(p) = true
				}
			}
			_, err := cfg.UserManager.GenerateInviteLink(req.FormValue("invite-name"), bc.FullUser, perms)
			if err != nil {
				var verifyErr *userauth.ErrorInviteLinkVerify
				if errors.As(err, &verifyErr) {
					return nil, httputil.MakeError(http.StatusForbidden, verifyErr.Unwrap().Error())
				}
				log.Error("could not create invite link", slogx.Err(err))
				return nil, fmt.Errorf("create invite link: %w", err)
			}
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "invite link generated", "/invites")
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
