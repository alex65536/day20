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

func (i *Info) PlayerInfo(col chess.Color) string {
	if col == chess.ColorWhite {
		return i.WhiteName
	}
	return i.BlackName
}

func (i *Info) Clone() *Info {
	if i == nil {
		return nil
	}
	res := *i
	res.TimeControl = util.CloneMaybe(res.TimeControl)
	return &res
}

type Position struct {
	Board   *chess.Board  `json:"board"`
	Status  chess.Status  `json:"status"`
	Verdict chess.Verdict `json:"verdict,omitempty"`
	Version int64         `json:"v"`
}

func (p *Position) Clone() *Position {
	if p == nil {
		return nil
	}
	res := *p
	res.Board = res.Board.Clone()
	return &res
}

type Moves struct {
	Moves   []chess.UCIMove          `json:"moves"`
	Scores  []maybe.Maybe[uci.Score] `json:"scores"`
	Version int64                    `json:"v"`
}

func (m *Moves) Clone() *Moves {
	if m == nil {
		return nil
	}
	res := *m
	res.Moves = slices.Clone(res.Moves)
	res.Scores = slices.Clone(res.Scores)
	return &res
}

func (m *Moves) Delta(old int64) *Moves {
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
	if m.Version+int64(len(d.Moves)) != d.Version || m.Version+int64(len(d.Scores)) != d.Version {
		return fmt.Errorf("bad delta length")
	}
	m.Moves = append(m.Moves, d.Moves...)
	m.Scores = append(m.Scores, d.Scores...)
	m.Version = d.Version
	return nil
}

type Warnings struct {
	Warn    []string `json:"warn"`
	Version int64    `json:"v"`
}

func (w *Warnings) Clone() *Warnings {
	if w == nil {
		return nil
	}
	res := *w
	res.Warn = slices.Clone(res.Warn)
	return &res
}

func (w *Warnings) Delta(old int64) *Warnings {
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
	if w.Version+int64(len(d.Warn)) != d.Version {
		return fmt.Errorf("bad delta length")
	}
	w.Warn = append(w.Warn, d.Warn...)
	w.Version = d.Version
	return nil
}

type Player struct {
	Active   bool                       `json:"active"`
	Clock    maybe.Maybe[time.Duration] `json:"clock"`
	Deadline maybe.Maybe[Timestamp]     `json:"deadline"`
	Score    maybe.Maybe[uci.Score]     `json:"score"`
	PV       []chess.UCIMove            `json:"pv"`
	PVS      string                     `json:"pvs"`
	Depth    int64                      `json:"depth"`
	Nodes    int64                      `json:"nodes"`
	NPS      int64                      `json:"nps"`
	Version  int64                      `json:"v"`
}

func (p *Player) ClockFrom(nowTs Timestamp) maybe.Maybe[time.Duration] {
	if d, ok := p.Deadline.TryGet(); ok {
		return maybe.Some(d.Sub(nowTs))
	}
	return p.Clock
}

func (p *Player) Clone() *Player {
	if p == nil {
		return nil
	}
	res := *p
	res.PV = slices.Clone(res.PV)
	return &res
}

func (p *Player) FixTimestamps(diff TimestampDiff) {
	if d, ok := p.Deadline.TryGet(); ok {
		p.Deadline = maybe.Some(FixTimestamp(diff, d))
	}
}

type JobCursor struct {
	HasInfo  bool  `json:"has_info"`
	Warnings int64 `json:"warnings"`
	Position int64 `json:"position"`
	Moves    int64 `json:"moves"`
	White    int64 `json:"white"`
	Black    int64 `json:"black"`
}

func b2i(b bool) int {
	if !b {
		return 0
	} else {
		return 1
	}
}

func (c JobCursor) Player(col chess.Color) int64 {
	if col == chess.ColorWhite {
		return c.White
	}
	return c.Black
}

func (c JobCursor) StrictLessEq(d JobCursor) bool {
	return b2i(c.HasInfo) <= b2i(d.HasInfo) &&
		c.Warnings <= d.Warnings &&
		c.Position <= d.Position &&
		c.Moves <= d.Moves &&
		c.White <= d.White &&
		c.Black <= d.Black
}

type JobState struct {
	Info     *Info     `json:"info,omitempty"`
	Warnings *Warnings `json:"warnings,omitempty"`
	Position *Position `json:"position,omitempty"`
	Moves    *Moves    `json:"moves,omitempty"`
	White    *Player   `json:"white,omitempty"`
	Black    *Player   `json:"black,omitempty"`
}

func NewJobState() *JobState {
	return &JobState{
		Info:     nil,
		Warnings: &Warnings{},
		Position: &Position{},
		Moves:    &Moves{},
		White:    &Player{},
		Black:    &Player{},
	}
}

func (s *JobState) Player(col chess.Color) *Player {
	if col == chess.ColorWhite {
		return s.White
	}
	return s.Black
}

func (s *JobState) ValidateFull() error {
	if s.Warnings == nil ||
		s.Position == nil ||
		s.Moves == nil ||
		s.White == nil ||
		s.Black == nil {
		return fmt.Errorf("state incomplete")
	}
	return nil
}

func (s *JobState) FixTimestamps(diff TimestampDiff) {
	if s.White != nil {
		s.White.FixTimestamps(diff)
	}
	if s.Black != nil {
		s.Black.FixTimestamps(diff)
	}
}

func (s *JobState) Cursor() JobCursor {
	return JobCursor{
		HasInfo:  s.Info != nil,
		Warnings: s.Warnings.Version,
		Position: s.Position.Version,
		Moves:    s.Moves.Version,
		White:    s.White.Version,
		Black:    s.Black.Version,
	}
}

func (s *JobState) Clone() *JobState {
	if s == nil {
		return nil
	}
	return &JobState{
		Info:     s.Info.Clone(),
		Warnings: s.Warnings.Clone(),
		Position: s.Position.Clone(),
		Moves:    s.Moves.Clone(),
		White:    s.White.Clone(),
		Black:    s.Black.Clone(),
	}
}

func (s *JobState) Delta(old JobCursor) (*JobState, error) {
	if !old.StrictLessEq(s.Cursor()) {
		return nil, fmt.Errorf("old cursor is not a parent of the current one")
	}
	res := &JobState{}
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

func (s *JobState) ApplyDelta(d *JobState) error {
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

func (s *JobState) GameExt() (*battle.GameExt, error) {
	if err := s.ValidateFull(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if s.Info == nil {
		return nil, fmt.Errorf("game not ready")
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

type RoomCursor struct {
	JobID string    `json:"job_id"`
	State JobCursor `json:"s"`
}

type RoomState struct {
	JobID string    `json:"job_id"`
	State *JobState `json:"s"`
}

func NewRoomState() *RoomState {
	return &RoomState{
		JobID: "",
		State: nil,
	}
}
func (s *RoomState) Cursor() RoomCursor {
	var state JobCursor
	if s.State != nil {
		state = s.State.Cursor()
	}
	return RoomCursor{
		JobID: s.JobID,
		State: state,
	}
}

func (s *RoomState) Clone() *RoomState {
	if s == nil {
		return nil
	}
	return &RoomState{
		JobID: s.JobID,
		State: s.State.Clone(),
	}
}

func (s *RoomState) doValidate() error {
	if s.State == nil && s.JobID != "" {
		return fmt.Errorf("has job but empty state")
	}
	if s.State != nil && s.JobID == "" {
		return fmt.Errorf("no job but non-empty state")
	}
	return nil
}

func (s *RoomState) ValidateFull() error {
	if err := s.doValidate(); err != nil {
		return err
	}
	if s.State != nil {
		if err := s.State.ValidateFull(); err != nil {
			return fmt.Errorf("inner state: %w", err)
		}
	}
	return nil
}

func (s *RoomState) ValidateDelta() error {
	return s.doValidate()
}

func (s *RoomState) Delta(old RoomCursor) (*RoomState, error) {
	if err := s.ValidateFull(); err != nil {
		return nil, fmt.Errorf("invalid state: %w", err)
	}
	if s.JobID != old.JobID {
		return &RoomState{
			JobID: s.JobID,
			State: s.State.Clone(),
		}, nil
	}
	if s.State == nil {
		return &RoomState{
			JobID: s.JobID,
			State: nil,
		}, nil
	}
	d, err := s.State.Delta(old.State)
	if err != nil {
		return nil, fmt.Errorf("job delta: %w", err)
	}
	return &RoomState{
		JobID: s.JobID,
		State: d,
	}, nil
}

func (s *RoomState) ApplyDelta(d *RoomState) error {
	if err := s.ValidateFull(); err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}
	if err := d.ValidateDelta(); err != nil {
		return fmt.Errorf("invalid delta: %w", err)
	}
	if s.JobID != d.JobID {
		if d.State != nil {
			if err := d.State.ValidateFull(); err != nil {
				return fmt.Errorf("invalid inner state: %w", err)
			}
		}
		s.JobID = d.JobID
		s.State = d.State.Clone()
		return nil
	}
	if s.State == nil {
		return nil
	}
	if err := s.State.ApplyDelta(d.State); err != nil {
		return fmt.Errorf("apply inner delta: %w", err)
	}
	return nil
}
