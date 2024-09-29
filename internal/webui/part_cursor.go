package webui

import (
	"encoding/json"
	"html/template"
	"log/slog"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/util/maybe"
)

type cursorPartData struct {
	JSON         string
	ForceRefresh bool
	AJAXAttrs    template.HTML
}

func buildCursorPartData(log *slog.Logger, cursor maybe.Maybe[delta.RoomCursor], forceRefresh bool) *cursorPartData {
	jsonData := "{}"
	if cursor.IsSome() {
		jsonBytes, err := json.Marshal(struct {
			C delta.RoomCursor `json:"c"`
		}{C: cursor.Get()})
		if err != nil {
			log.Error("could not marshal cursor", slogx.Err(err))
		} else {
			jsonData = string(jsonBytes)
		}
	}
	return &cursorPartData{
		JSON:         jsonData,
		ForceRefresh: forceRefresh,
	}
}
