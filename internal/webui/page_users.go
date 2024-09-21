package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/userauth"
	"github.com/alex65536/day20/internal/util/sliceutil"
)

type usersDataItem struct {
	Username string
}

type usersData struct {
	Users []usersDataItem
}

type usersDataBuilder struct{}

func (usersDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config

	users, err := cfg.UserManager.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	items := sliceutil.Map(users, func(u userauth.User) usersDataItem {
		return usersDataItem{
			Username: u.Username,
		}
	})

	return &usersData{
		Users: items,
	}, nil
}

func usersPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, usersDataBuilder{}, "users")
}
