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

type loginDataBuilder struct{}

func (loginDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	cfg := bc.Config
	log := bc.Log

	type data struct {
		CSRFField template.HTML
	}

	if bc.UserInfo != nil {
		return nil, bc.Redirect("/")
	}

	switch req.Method {
	case http.MethodGet:
		return &data{
			CSRFField: csrf.TemplateField(req),
		}, nil
	case http.MethodPost:
		if !bc.IsHTMX() {
			return nil, httputil.MakeError(http.StatusBadRequest, "must use htmx request")
		}
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
				log.Warn("could not get user", slogx.Err(err))
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
			return &errorsPartData{Errors: []string{strErr}}, nil
		}
		bc.ResetSession(makeUserInfo(&user))
		return nil, bc.Redirect("/")
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func loginPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, loginDataBuilder{}, "login")
}
