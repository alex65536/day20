package webui

import (
	"html/template"

	"github.com/alex65536/go-chess/chess"
)

type fenPartData struct {
	FEN       string
	AJAXAttrs template.HTML
}

func buildFENPartData(board *chess.Board) *fenPartData {
	data := &fenPartData{
		FEN: "8/8/8/8/8/8/8/8 w - - 0 1",
	}
	if board != nil {
		data.FEN = board.FEN()
	}
	return data
}
