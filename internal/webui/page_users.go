package webui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/util/sliceutil"
)

type usersDataBuilder struct{}

func (usersDataBuilder) Build(ctx context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config

	type data struct {
		Users []*userPartData
	}

	users, err := cfg.UserManager.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	return &data{
		Users: sliceutil.Map(users, buildUserPartData),
	}, nil
}

func usersPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, usersDataBuilder{}, "users")
}
