package webui

import (
	"context"
	"crypto/subtle"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
	"github.com/gorilla/csrf"
)

type inviteData struct {
	InviteVal string
	Errors    []string
	CSRFField template.HTML
}

type inviteDataBuilder struct{}

func (inviteDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	if bc.UserInfo != nil {
		return nil, httputil.MakeError(http.StatusBadRequest, "already logged in")
	}

	inviteVal := req.PathValue("inviteVal")
	inviteHash := userauth.HashInviteValue(inviteVal)
	lnk, err := cfg.UserManager.GetInviteLink(ctx, inviteHash, timeutil.NowUTC())
	if err != nil || subtle.ConstantTimeCompare([]byte(lnk.Value), []byte(inviteVal)) == 0 {
		log.Info("could not get invite link", slogx.Err(err))
		return nil, httputil.MakeError(http.StatusNotFound, "invite link not found")
	}

	switch req.Method {
	case http.MethodGet:
		return &inviteData{
			InviteVal: inviteVal,
			Errors:    nil,
			CSRFField: csrf.TemplateField(req),
		}, nil
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		user, errs := func() (userauth.User, []string) {
			var errs []string
			username, password, password2 := req.FormValue("username"), req.FormValue("password"), req.FormValue("password2")
			if subtle.ConstantTimeCompare([]byte(password), []byte(password2)) == 0 {
				errs = append(errs, "passwords mismatch")
			}
			if err := userauth.ValidatePassword(password); err != nil {
				errs = append(errs, err.Error())
			}
			if err := userauth.ValidateUsername(username); err != nil {
				errs = append(errs, err.Error())
			}
			if len(errs) != 0 {
				return userauth.User{}, errs
			}
			user := userauth.User{
				ID:        idgen.ID(),
				Username:  username,
				InviterID: clone.TrivialPtr(lnk.OwnerUserID),
				Perms:     lnk.Perms,
			}
			if err := cfg.UserManager.SetPassword(&user, []byte(password)); err != nil {
				log.Error("could not set password to user", slogx.Err(err))
				return userauth.User{}, []string{"internal server error"}
			}
			if err := cfg.UserManager.CreateUser(ctx, user, lnk); err != nil {
				if errors.Is(err, userauth.ErrInviteLinkUsed) {
					return userauth.User{}, []string{"invite link already used"}
				}
				if errors.Is(err, userauth.ErrUserAlreadyExists) {
					return userauth.User{}, []string{"given username is already taken"}
				}
				log.Error("could not create user in db", slogx.Err(err))
				return userauth.User{}, []string{"internal server error"}
			}
			return user, nil
		}()
		if len(errs) > 0 {
			return &inviteData{
				InviteVal: inviteVal,
				Errors:    errs,
				CSRFField: csrf.TemplateField(req),
			}, nil
		}
		bc.ResetSession(makeUserInfo(&user))
		return nil, httputil.MakeRedirectError(http.StatusSeeOther, "login successful, redirecting", "/")
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func invitePage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoShowAuth: true}, templ, inviteDataBuilder{}, "invite")
}
