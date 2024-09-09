package delta

import (
	"fmt"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/util"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type Info struct {
	WhiteName   string                     `json:"white_name"`
	BlackName   string                     `json:"black_name"`
	StartPos    chess.RawBoard             `json:"start_pos"`
	TimeControl maybe.Maybe[clock.Control] `json:"time_control"`
	FixedTime   maybe.Maybe[time.Duration] `json:"fixed_time"`
}

func (i *Info) Clone() *Info {
	res := *i
	res.TimeControl = util.CloneMaybe(res.TimeControl)
	return &res
}

type Position struct {
	Board   *chess.Board  `json:"board"`
	Status  chess.Status  `json:"status"`
	Verdict chess.Verdict `json:"verdict,omitempty"`
	Version int           `json:"v"`
}

func (p *Position) Clone() *Position {
	res := *p
	res.Board = res.Board.Clone()
	return &res
}

type Moves struct {
	Moves   []chess.UCIMove          `json:"moves"`
	Scores  []maybe.Maybe[uci.Score] `json:"scores"`
	Version int                      `json:"v"`
}

func (m *Moves) Clone() *Moves {
	res := *m
	res.Moves = slices.Clone(res.Moves)
	res.Scores = slices.Clone(res.Scores)
	return &res
}

func (m *Moves) Delta(old int) *Moves {
	if old == m.Version {
		return nil
	}
	if old < 0 || old > m.Version {
		panic("must not happen")
	}
	return &Moves{
		Moves:   slices.Clone(m.Moves[old:m.Version]),
		Scores:  slices.Clone(m.Scores[old:m.Version]),
		Version: m.Version,
	}
}

func (m *Moves) ApplyDelta(d *Moves) error {
	if m.Version >= d.Version {
		return fmt.Errorf("already up-to-date")
	}
	if m.Version+len(d.Moves) != d.Version || m.Version+len(d.Scores) != d.Version {
		return fmt.Errorf("bad delta length")
	}
	m.Moves = append(m.Moves, d.Moves...)
	m.Scores = append(m.Scores, d.Scores...)
	m.Version = d.Version
	return nil
}

type Warnings struct {
	Warn    []string `json:"warn"`
	Version int      `json:"v"`
}

func (w *Warnings) Delta(old int) *Warnings {
	if old == w.Version {
		return nil
	}
	if old < 0 || old > w.Version {
		panic("must not happen")
	}
	return &Warnings{
		Warn: slices.Clone(w.Warn[old:w.Version]),
	}
}

func (w *Warnings) ApplyDelta(d *Warnings) error {
	if w.Version >= d.Version {
		return fmt.Errorf("already up-to-date")
	}
	if w.Version+len(d.Warn) != d.Version {
		return fmt.Errorf("bad delta length")
	}
	w.Warn = append(w.Warn, d.Warn...)
	w.Version = d.Version
	return nil
}

type Player struct {
	Active          bool                       `json:"active"`
	Clock           maybe.Maybe[time.Duration] `json:"clock"`
	ClockUpdateTime time.Time                  `json:"-"`
	Score           maybe.Maybe[uci.Score]     `json:"score"`
	PV              []chess.UCIMove            `json:"pv"`
	Depth           int                        `json:"depth"`
	Nodes           int64                      `json:"nodes"`
	NPS             int64                      `json:"nps"`
	Version         int                        `json:"v"`
}

func (p *Player) Clone() *Player {
	res := *p
	res.PV = slices.Clone(res.PV)
	return &res
}

type Cursor struct {
	HasInfo  bool `json:"has_info"`
	Warnings int  `json:"warnings"`
	Position int  `json:"position"`
	Moves    int  `json:"moves"`
	White    int  `json:"white"`
	Black    int  `json:"black"`
}

func b2i(b bool) int {
	if !b {
		return 0
	} else {
		return 1
	}
}

func (c Cursor) StrictLessEq(d Cursor) bool {
	return b2i(c.HasInfo) <= b2i(d.HasInfo) &&
		c.Warnings <= d.Warnings &&
		c.Position <= d.Position &&
		c.Moves <= d.Moves &&
		c.White <= d.White &&
		c.Black <= d.Black
}

type State struct {
	Info     *Info     `json:"info,omitempty"`
	Warnings *Warnings `json:"warnings,omitempty"`
	Position *Position `json:"position,omitempty"`
	Moves    *Moves    `json:"moves,omitempty"`
	White    *Player   `json:"white,omitempty"`
	Black    *Player   `json:"black,omitempty"`
}

func NewState() *State {
	return &State{
		Info:     nil,
		Warnings: &Warnings{},
		Position: &Position{},
		Moves:    &Moves{},
		White:    &Player{},
		Black:    &Player{},
	}
}

func (s *State) Cursor() Cursor {
	return Cursor{
		HasInfo:  s.Info != nil,
		Warnings: s.Warnings.Version,
		Position: s.Position.Version,
		Moves:    s.Moves.Version,
		White:    s.White.Version,
		Black:    s.Black.Version,
	}
}

func (s *State) Delta(old Cursor) (*State, error) {
	if !old.StrictLessEq(s.Cursor()) {
		return nil, fmt.Errorf("old cursor is not a parent of the current one")
	}
	res := &State{}
	if s.Info != nil && !old.HasInfo {
		res.Info = s.Info.Clone()
	}
	if s.Warnings != nil && old.Warnings != s.Warnings.Version {
		res.Warnings = s.Warnings.Delta(old.Warnings)
	}
	if s.Position != nil && old.Position != s.Position.Version {
		res.Position = s.Position.Clone()
	}
	if s.Moves != nil && old.Moves != s.Moves.Version {
		res.Moves = s.Moves.Delta(old.Moves)
	}
	if s.White != nil && old.White != s.White.Version {
		res.White = s.White.Clone()
	}
	if s.Black != nil && old.Black != s.Black.Version {
		res.Black = s.Black.Clone()
	}
	return res, nil
}

func (s *State) ApplyDelta(d *State) error {
	if d.Info != nil {
		if s.Info != nil {
			return fmt.Errorf("info already present")
		}
		s.Info = d.Info.Clone()
	}
	if d.Warnings != nil {
		if err := s.Warnings.ApplyDelta(d.Warnings); err != nil {
			return fmt.Errorf("apply warnings: %w", err)
		}
	}
	if d.Position != nil {
		if s.Position.Version >= d.Position.Version {
			return fmt.Errorf("position already up-to-date")
		}
		s.Position = d.Position.Clone()
	}
	if d.Moves != nil {
		if err := s.Moves.ApplyDelta(d.Moves); err != nil {
			return fmt.Errorf("apply moves: %w", err)
		}
	}
	if d.White != nil {
		if s.White.Version >= d.White.Version {
			return fmt.Errorf("white already up-to-date")
		}
		s.White = d.White.Clone()
	}
	if d.Black != nil {
		if s.Black.Version >= d.Black.Version {
			return fmt.Errorf("black already up-to-date")
		}
		s.Black = d.Black.Clone()
	}
	return nil
}

func (s *State) GameExt() (*battle.GameExt, error) {
	if s.Info == nil || s.Position == nil || s.Moves == nil {
		return nil, fmt.Errorf("state not initialized or is delta")
	}

	board, err := chess.NewBoard(s.Info.StartPos)
	if err != nil {
		return nil, fmt.Errorf("bad start pos: %w", err)
	}
	game := chess.NewGameWithPosition(board)
	for i, mv := range s.Moves.Moves {
		if err := game.PushUCIMove(mv); err != nil {
			return nil, fmt.Errorf("bad move #%d %v: %w", i+1, mv, err)
		}
	}
	outcome, ok := func(status chess.Status, verdict chess.Verdict) (chess.Outcome, bool) {
		switch status {
		case chess.StatusRunning:
			if s.Position.Verdict != chess.VerdictRunning {
				return chess.Outcome{}, false
			}
			return chess.RunningOutcome(), true
		case chess.StatusDraw:
			return chess.DrawOutcome(verdict)
		case chess.StatusWhiteWins, chess.StatusBlackWins:
			w, _ := status.Winner()
			return chess.WinOutcome(verdict, w)
		default:
			panic("must not happen")
		}
	}(s.Position.Status, s.Position.Verdict)
	if !ok {
		return nil, fmt.Errorf("bad status %v to match verdict %v", s.Position.Status, s.Position.Verdict)
	}
	game.SetOutcome(outcome)

	return &battle.GameExt{
		Game:        game,
		Scores:      slices.Clone(s.Moves.Scores),
		WhiteName:   s.Info.WhiteName,
		BlackName:   s.Info.BlackName,
		Round:       0,
		TimeControl: util.CloneMaybe(s.Info.TimeControl),
		FixedTime:   s.Info.FixedTime,
	}, nil
}
