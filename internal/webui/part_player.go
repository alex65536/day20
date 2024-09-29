package webui

import (
	"html/template"
	"strconv"

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
	Depth     string
	Nodes     string
	NPS       string
	AJAXAttrs template.HTML
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
		Depth:     "-",
		Nodes:     "-",
		NPS:       "-",
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
	if player.Depth != 0 {
		data.Depth = strconv.FormatInt(int64(player.Depth), 10)
	}
	if player.Nodes != 0 {
		data.Nodes = strconv.FormatInt(player.Nodes, 10)
	}
	if player.NPS != 0 {
		data.NPS = strconv.FormatInt(player.NPS, 10)
	}
	return data
}
