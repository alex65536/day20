package webui

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/gorilla/csrf"
)

type userDataBuilder struct{}

func (userDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	ourUser := bc.FullUser
	cfg := bc.Config
	log := bc.Log

	type data struct {
		User              *userPartData
		CSRFField         template.HTML
		CanChangePassword bool
		CanChangePerms    bool
		CanInvite         bool
		CanHostRooms      bool
	}

	targetUsername := req.PathValue("username")
	targetUser, err := cfg.UserManager.GetUserByUsername(ctx, targetUsername)
	if err != nil {
		if errors.Is(err, userauth.ErrUserNotFound) {
			return nil, httputil.MakeError(http.StatusNotFound, "user not found")
		}
		log.Warn("could not fetch target user", slogx.Err(err))
		return nil, fmt.Errorf("fetch target user: %w", err)
	}

	canChangePerms := false
	if ourUser != nil {
		err := targetUser.CanChangePerms(ourUser, targetUser.Perms)
		canChangePerms = err == nil
	}
	isOurOwnPage := ourUser != nil && ourUser.ID == targetUser.ID
	canChangePassword := isOurOwnPage && !ourUser.Perms.IsBlocked

	switch req.Method {
	case http.MethodGet:
		return &data{
			User:              buildUserPartData(targetUser),
			CSRFField:         csrf.TemplateField(req),
			CanChangePassword: canChangePassword,
			CanChangePerms:    canChangePerms,
			CanInvite:         isOurOwnPage && ourUser.Perms.Get(userauth.PermInvite),
			CanHostRooms:      isOurOwnPage && ourUser.Perms.Get(userauth.PermHostRooms),
		}, nil
	case http.MethodPost:
		if !bc.IsHTMX() {
			return nil, httputil.MakeError(http.StatusBadRequest, "must use htmx request")
		}
		err := req.ParseForm()
		if err != nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "bad form data")
		}
		if ourUser == nil {
			return nil, httputil.MakeError(http.StatusBadRequest, "not logged in")
		}
		switch req.FormValue("action") {
		case "password":
			oldPassword := req.FormValue("old-password")
			newPassword, newPassword2 := req.FormValue("new-password"), req.FormValue("new-password2")
			serr := func() string {
				if !canChangePassword {
					return "operation not permitted"
				}
				if !cfg.UserManager.VerifyPassword(ourUser, []byte(oldPassword)) {
					return "invalid password"
				}
				if subtle.ConstantTimeCompare([]byte(newPassword), []byte(newPassword2)) == 0 {
					return "new passwords do not match"
				}
				if err := userauth.ValidatePassword(newPassword); err != nil {
					return err.Error()
				}
				if err := cfg.UserManager.SetPassword(ourUser, []byte(newPassword)); err != nil {
					log.Warn("could not change password", slogx.Err(err))
					return "internal server error"
				}
				if err := cfg.UserManager.UpdateUser(ctx, *ourUser); err != nil {
					log.Warn("could not save user", slogx.Err(err))
					return "internal server error"
				}
				bc.UpgradeSession(makeUserInfo(ourUser))
				return ""
			}()
			if serr != "" {
				return &errorsPartData{
					Errors: []string{serr},
				}, nil
			}
			return nil, bc.Redirect("/user/" + targetUsername)
		case "perms":
			serr := func() string {
				var perms userauth.Perms
				for p := range userauth.PermMax {
					*perms.GetMut(p) = req.FormValue("perm-"+p.String()) == "true"
				}
				if req.FormValue("perm-blocked") == "true" {
					perms = userauth.BlockedPerms()
				}
				if err := targetUser.TryChangePerms(ourUser, perms); err != nil {
					return err.Error()
				}
				if err := cfg.UserManager.UpdateUser(ctx, targetUser, userauth.UpdateUserOptions{
					InvalidatePerms: true,
				}); err != nil {
					log.Warn("could not save user", slogx.Err(err))
					return "internal server error"
				}
				return ""
			}()
			if serr != "" {
				return &errorsPartData{
					Errors: []string{serr},
				}, nil
			}
			return nil, bc.Redirect("/user/" + targetUsername)
		default:
			return nil, httputil.MakeError(http.StatusBadRequest, "unknown action")
		}
	default:
		return nil, httputil.MakeError(http.StatusMethodNotAllowed, "method not allowed")
	}
}

func userPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{FullUser: true}, templ, userDataBuilder{}, "user")
}
