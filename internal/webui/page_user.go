package webui

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/httputil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/gorilla/csrf"
)

type userPerm struct {
	Active bool
	Field  string
	Name   string
}

type userData struct {
	Username             string
	PermStr              string
	CSRFField            template.HTML
	CanChangePassword    bool
	ChangePasswordErrors []string
	CanChangePerms       bool
	ChangePermsErrors    []string
	Perms                []userPerm
	UserBlocked          bool
	CanInvite            bool
	CanHostRooms         bool
}

type userDataBuilder struct{}

func (userDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	req := bc.Req
	ourUser := bc.FullUser
	cfg := bc.Config
	log := bc.Log

	targetUsername := req.PathValue("username")
	targetUser, err := cfg.UserManager.GetUserByUsername(ctx, targetUsername)
	if err != nil {
		if errors.Is(err, userauth.ErrUserNotFound) {
			return nil, httputil.MakeError(http.StatusNotFound, "user not found")
		}
		log.Error("could not fetch target user", slogx.Err(err))
		return nil, fmt.Errorf("fetch target user: %w", err)
	}

	permStrs := []string{}
	if targetUser.Perms.IsBlocked {
		permStrs = append(permStrs, "Blocked")
	} else {
		if targetUser.Perms.IsOwner {
			permStrs = append(permStrs, "Owner")
		}
		for perm := range userauth.PermMax {
			if targetUser.Perms.Get(perm) {
				permStrs = append(permStrs, perm.PrettyString())
			}
		}
	}

	canChangePerms := false
	if ourUser != nil {
		err := targetUser.CanChangePerms(ourUser, targetUser.Perms)
		canChangePerms = err == nil
	}
	isOurOwnPage := ourUser != nil && ourUser.ID == targetUser.ID
	perms := []userPerm{}
	if canChangePerms {
		for perm := range userauth.PermMax {
			perms = append(perms, userPerm{
				Active: targetUser.Perms.Get(perm),
				Field:  "perm-" + perm.String(),
				Name:   perm.PrettyString(),
			})
		}
	}

	data := &userData{
		Username:          targetUser.Username,
		PermStr:           strings.Join(permStrs, ", "),
		CSRFField:         csrf.TemplateField(req),
		CanChangePassword: isOurOwnPage && !ourUser.Perms.IsBlocked,
		CanChangePerms:    canChangePerms,
		Perms:             perms,
		UserBlocked:       targetUser.Perms.IsBlocked,
		CanInvite:         isOurOwnPage && ourUser.Perms.Get(userauth.PermInvite),
		CanHostRooms:      isOurOwnPage && ourUser.Perms.Get(userauth.PermHostRooms),
	}

	switch req.Method {
	case http.MethodGet:
		return data, nil
	case http.MethodPost:
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
				if !data.CanChangePassword {
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
					log.Error("could not change password", slogx.Err(err))
					return "internal server error"
				}
				if err := cfg.UserManager.UpdateUser(ctx, *ourUser); err != nil {
					log.Error("could not save user", slogx.Err(err))
					return "internal server error"
				}
				bc.UpgradeSession(makeUserInfo(ourUser))
				return ""
			}()
			if serr != "" {
				data.ChangePasswordErrors = []string{serr}
				return data, nil
			}
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "password changed", "/user/"+targetUsername)
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
					log.Error("could not save user", slogx.Err(err))
					return "internal server error"
				}
				return ""
			}()
			if serr != "" {
				data.ChangePermsErrors = []string{serr}
				return data, nil
			}
			return nil, httputil.MakeRedirectError(http.StatusSeeOther, "perms changed", "/user/"+targetUsername)
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
