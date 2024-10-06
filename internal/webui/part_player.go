package webui

import (
	"html/template"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/go-chess/chess"
)

type playerClockData struct {
	Msecs  int64
	Active bool
}

type playerPartData struct {
	Color     string
	ColorText string
	ClockVar  template.JS
	Name      string
	Active    bool
	Clock     *playerClockData
	Score     string
	PV        string
	Depth     int64
	Nodes     int64
	NPS       int64
	AJAXAttrs template.HTMLAttr
}

func colorText(col chess.Color) string {
	if col == chess.ColorWhite {
		return "White"
	}
	return "Black"
}

func buildPlayerPartData(col chess.Color, state *delta.JobState) *playerPartData {
	playerName := ""
	if state != nil && state.Info != nil {
		playerName = state.Info.PlayerInfo(col)
	}
	data := &playerPartData{
		Color:     col.LongString(),
		ColorText: colorText(col),
		ClockVar:  template.JS(col.LongString() + "Clock"),
		Name:      playerName,
		Active:    false,
		Clock:     nil,
		Score:     "-",
		PV:        "",
		Depth:     0,
		Nodes:     0,
		NPS:       0,
	}
	var player *delta.Player
	if state != nil {
		player = state.Player(col)
	}
	if player == nil {
		return data
	}
	data.Active = player.Active
	if c, ok := player.ClockFrom(delta.NowTimestamp()).TryGet(); ok {
		data.Clock = &playerClockData{
			Active: player.Active,
			Msecs:  c.Milliseconds(),
		}
	}
	if s, ok := player.Score.TryGet(); ok {
		data.Score = s.String()
	}
	data.PV = player.PVS
	data.Depth = player.Depth
	data.Nodes = player.Nodes
	data.NPS = player.NPS
	return data
}
