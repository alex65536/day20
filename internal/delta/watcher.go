package delta

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type Watcher struct {
	mu       sync.RWMutex
	state    *State
	notifyCh chan<- struct{}
	done     chan struct{}
}

type WatcherOptions struct {
	ClockUpdateInterval time.Duration
}

func (o *WatcherOptions) FillDefaults() {
	if o.ClockUpdateInterval == 0 {
		o.ClockUpdateInterval = 3 * time.Second
	}
}

var _ battle.Watcher = (*Watcher)(nil)

func NewWatcher(o WatcherOptions) (*Watcher, <-chan struct{}) {
	notifyCh := make(chan struct{}, 1)
	w := &Watcher{
		state:    NewState(),
		notifyCh: notifyCh,
		done:     make(chan struct{}, 1),
	}
	go func() {
		t := time.NewTicker(o.ClockUpdateInterval)
		defer t.Stop()
	loop:
		for {
			select {
			case <-t.C:
				w.UpdateTimer()
			case <-w.done:
				break loop
			}
		}
	}()
	return w, notifyCh
}

func (w *Watcher) startTx() Cursor {
	w.mu.Lock()
	return w.state.Cursor()
}

func (w *Watcher) endTx(c Cursor) {
	newC := w.state.Cursor()
	w.mu.Unlock()
	if c != newC {
		select {
		case w.notifyCh <- struct{}{}:
		default:
		}
	}
}

func (w *Watcher) Done() <-chan struct{} {
	return w.done
}

func (w *Watcher) OnGameInited(game *battle.GameExt) {
	cursor := w.startTx()
	defer w.endTx(cursor)

	w.state.Info = &Info{
		WhiteName:   game.WhiteName,
		BlackName:   game.BlackName,
		StartPos:    game.Game.StartPos(),
		TimeControl: game.TimeControl,
		FixedTime:   game.FixedTime,
	}

	board, err := chess.NewBoard(game.Game.StartPos())
	if err != nil {
		panic("must not happen")
	}
	w.state.Position = &Position{
		Board:   board,
		Status:  game.Game.Outcome().Status(),
		Verdict: game.Game.Outcome().Verdict(),
		Version: 1,
	}

	w.updateGameUnlocked(game)
}

func (w *Watcher) updateGameUnlocked(game *battle.GameExt) {
	if len(game.Scores) != game.Game.Len() {
		panic("must not happen")
	}

	oldLen, newLen := w.state.Moves.Version, len(game.Scores)
	for i := oldLen; i < newLen; i++ {
		move := game.Game.MoveAt(i)
		w.state.Moves.Moves = append(w.state.Moves.Moves, move.UCIMove())
		_ = w.state.Position.Board.MakeLegalMove(move)
		w.state.Position.Version++
	}
	w.state.Moves.Scores = append(w.state.Moves.Scores, game.Scores[oldLen:newLen]...)
	w.state.Moves.Version = newLen

	status := game.Game.Outcome().Status()
	verdict := game.Game.Outcome().Verdict()
	if w.state.Position.Status != status ||
		w.state.Position.Verdict != verdict {
		w.state.Position.Status = status
		w.state.Position.Verdict = verdict
		w.state.Position.Version++
	}
}

func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	select {
	case <-w.done:
	default:
		close(w.done)
		close(w.notifyCh)
	}
}

func (w *Watcher) OnGameFinished(game *battle.GameExt, warn battle.Warnings) {
	defer w.Close()

	cursor := w.startTx()
	defer w.endTx(cursor)

	w.updateGameUnlocked(game)
	if len(warn) != 0 {
		w.state.Warnings.Warn = slices.Clone(warn)
		w.state.Warnings.Version = len(warn)
	}
}

func (w *Watcher) OnEngineInfo(color chess.Color, status uci.SearchStatus) {
	cursor := w.startTx()
	defer w.endTx(cursor)

	var pl *Player
	if color == chess.ColorWhite {
		pl = w.state.White
	} else {
		pl = w.state.Black
	}

	if status.Score != pl.Score ||
		!slices.Equal(status.PV, pl.PV) ||
		status.Depth != pl.Depth ||
		status.Nodes != pl.Nodes ||
		status.NPS != pl.NPS {
		pl.Score = status.Score
		pl.PV = status.PV
		pl.Depth = status.Depth
		pl.Nodes = status.Nodes
		pl.NPS = status.NPS
		pl.Version++
	}
}

func (w *Watcher) OnGameUpdated(game *battle.GameExt, clk maybe.Maybe[clock.Clock]) {
	now := time.Now()

	cursor := w.startTx()
	defer w.endTx(cursor)

	w.updateGameUnlocked(game)

	if c, ok := clk.TryGet(); ok {
		w.state.White.Clock = maybe.Some(c.White)
		w.state.White.Active = c.WhiteTicking
		w.state.White.ClockUpdateTime = now
		w.state.White.Version++

		w.state.Black.Clock = maybe.Some(c.Black)
		w.state.Black.Active = c.BlackTicking
		w.state.Black.ClockUpdateTime = now
		w.state.Black.Version++
	}
}

func (w *Watcher) State(old Cursor) (*State, Cursor, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	d, err := w.state.Delta(old)
	if err != nil {
		return nil, Cursor{}, fmt.Errorf("delta: %w", err)
	}
	return d, w.state.Cursor(), nil
}

func (w *Watcher) UpdateTimer() {
	now := time.Now()

	cursor := w.startTx()
	defer w.endTx(cursor)

	for _, p := range []*Player{w.state.White, w.state.Black} {
		if p.Active && p.Clock.IsSome() {
			delta := now.Sub(p.ClockUpdateTime)
			p.Clock = maybe.Some(p.Clock.Get() - delta)
			p.ClockUpdateTime = now
			p.Version++
		}
	}
}
