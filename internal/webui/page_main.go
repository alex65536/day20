package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/roomkeeper"
)

type mainDataBuilder struct{}

func (mainDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config

	type data struct {
		Rooms []roomkeeper.RoomInfo
	}

	d := &data{}
	d.Rooms = cfg.Keeper.ListRooms()
	return d, nil
}

func mainPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, mainDataBuilder{}, "main")
}
