package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/roomkeeper"
)

type mainDataBuilder struct{}

func (mainDataBuilder) Build(ctx context.Context, log *slog.Logger, cfg *Config, req *http.Request) (any, error) {
	_ = ctx
	_ = log

	type data struct {
		Rooms []roomkeeper.RoomInfo
	}

	d := &data{}
	d.Rooms = cfg.Keeper.ListRooms()
	return d, nil
}

func mainPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, templ, mainDataBuilder{}, "main")
}
