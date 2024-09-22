package webui

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/gorilla/csrf"
)

type loginData struct {
	Errors    []string
	CSRFField template.HTML
}

type loginDataBuilder struct{}

func (loginDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	if bc.UserInfo != nil {
		return nil, httputil.MakeRedirectError(http.StatusSeeOther, "already logged in", "/")
	}

	switch req.Method {
	case http.MethodGet:
		return &loginData{
			Errors:    nil,
			CSRFField: csrf.TemplateField(req),
		}, nil
	case http.MethodPost:
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		user, strErr := func() (userauth.User, string) {
			username, password := req.FormValue("username"), req.FormValue("password")
			user, err := cfg.UserManager.GetUserByUsername(ctx, username)
			if err != nil {
				if errors.Is(err, userauth.ErrUserNotFound) {
					return userauth.User{}, "invalid username or password"
				}
				log.Error("could not get user", slogx.Err(err))
				return userauth.User{}, "internal server error"
			}
			if !cfg.UserManager.VerifyPassword(&user, []byte(password)) {
				return userauth.User{}, "invalid username or password"
			}
			if user.Perms.IsBlocked {
				return userauth.User{}, "user is blocked"
			}
			return user, ""
		}()
		if strErr != "" {
			return &loginData{
				Errors:    []string{strErr},
				CSRFField: csrf.TemplateField(req),
			}, nil
		}
		bc.ResetSession(makeUserInfo(&user))
		return nil, httputil.MakeRedirectError(http.StatusSeeOther, "login successful", "/")
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func loginPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{NoShowAuth: true}, templ, loginDataBuilder{}, "login")
}
