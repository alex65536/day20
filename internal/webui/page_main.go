package webui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/sliceutil"
)

type mainDataBuilder struct{}

func (mainDataBuilder) Build(_ context.Context, bc builderCtx) (any, error) {
	cfg := bc.Config

	type item struct {
		ID     string
		Name   string
		Active bool
	}

	type data struct {
		Rooms []item
	}

	d := &data{}
	d.Rooms = sliceutil.Map(cfg.Keeper.ListRooms(), func(s roomkeeper.RoomState) item {
		return item{ID: s.Info.ID, Name: s.Info.Name, Active: s.JobID.IsSome()}
	})
	return d, nil
}

func mainPage(log *slog.Logger, cfg *Config, templ *templator) (http.Handler, error) {
	return newPage(log, cfg, pageOptions{}, templ, mainDataBuilder{}, "main")
}
