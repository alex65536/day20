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
	o        WatcherOptions
	mu       sync.RWMutex
	state    *JobState
	notifyCh chan<- struct{}
	done     chan struct{}
}

type WatcherOptions struct {
	NoBuildPVS bool
	PassRawPV  bool
	MaxPVLen   int
}

func (o *WatcherOptions) FillDefaults() {
	if o.MaxPVLen == 0 {
		o.MaxPVLen = 16
	}
}

var _ battle.Watcher = (*Watcher)(nil)

func NewWatcher(o WatcherOptions) (*Watcher, <-chan struct{}) {
	o.FillDefaults()
	notifyCh := make(chan struct{}, 1)
	w := &Watcher{
		o:        o,
		state:    NewJobState(),
		notifyCh: notifyCh,
		done:     make(chan struct{}, 1),
	}
	return w, notifyCh
}

func (w *Watcher) startTx() JobCursor {
	w.mu.Lock()
	return w.state.Cursor()
}

func (w *Watcher) endTx(c JobCursor) {
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

	oldLen, newLen := int(w.state.Moves.Version), len(game.Scores)
	for i := oldLen; i < newLen; i++ {
		move := game.Game.MoveAt(i)
		w.state.Moves.Moves = append(w.state.Moves.Moves, move.UCIMove())
		_ = w.state.Position.Board.MakeLegalMove(move)
		w.state.Position.Version++
	}
	w.state.Moves.Scores = append(w.state.Moves.Scores, game.Scores[oldLen:newLen]...)
	w.state.Moves.Version = int64(newLen)

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
		w.state.Warnings.Version = int64(len(warn))
	}
}

func buildPVS(b *chess.Board, pv []chess.UCIMove) string {
	if b == nil || len(pv) == 0 {
		return ""
	}
	g := chess.NewGameWithPosition(b)
	corrupt := false
	for _, m := range pv {
		if err := g.PushUCIMove(m); err != nil {
			corrupt = true
			break
		}
	}
	pvs, err := g.Styled(chess.GameStyle{
		Move:       chess.MoveStyleFancySAN,
		MoveNumber: chess.MoveNumberStyle{Enabled: true},
		Outcome:    chess.GameOutcomeHide,
	})
	if err != nil {
		return "???"
	}
	if corrupt {
		if len(pvs) == 0 {
			pvs += " "
		}
		pvs += "???"
	}
	return pvs
}

func (w *Watcher) OnEngineInfo(color chess.Color, status uci.SearchStatus) {
	cursor := w.startTx()
	defer w.endTx(cursor)

	if len(status.PV) > w.o.MaxPVLen {
		status.PV = status.PV[:w.o.MaxPVLen]
	}

	var pl *Player
	if color == chess.ColorWhite {
		pl = w.state.White
	} else {
		pl = w.state.Black
	}
	if !pl.Active {
		panic("must not happen")
	}

	pvChanged := !slices.Equal(status.PV, pl.PV)
	if status.Score != pl.Score ||
		pvChanged ||
		int64(status.Depth) != pl.Depth ||
		status.Nodes != pl.Nodes ||
		status.NPS != pl.NPS {
		pl.Score = status.Score
		pl.PV = status.PV
		if !w.o.NoBuildPVS && pvChanged {
			pl.PVS = buildPVS(w.state.Position.Board, pl.PV)
		}
		pl.Depth = int64(status.Depth)
		pl.Nodes = status.Nodes
		pl.NPS = status.NPS
		pl.Version++
	}
}

func (w *Watcher) OnGameUpdated(game *battle.GameExt, clk maybe.Maybe[clock.Clock]) {
	nowTs := NowTimestamp()
	makeDeadline := func(ticking bool, d time.Duration) maybe.Maybe[Timestamp] {
		if ticking {
			return maybe.Some(nowTs.Add(d))
		}
		return maybe.None[Timestamp]()
	}

	cursor := w.startTx()
	defer w.endTx(cursor)

	w.updateGameUnlocked(game)

	if c, ok := clk.TryGet(); ok {
		w.state.White.Clock = maybe.Some(c.White)
		w.state.White.Active = c.WhiteTicking
		w.state.White.Deadline = makeDeadline(c.WhiteTicking, c.White)
		w.state.White.Version++

		w.state.Black.Clock = maybe.Some(c.Black)
		w.state.Black.Active = c.BlackTicking
		w.state.Black.Deadline = makeDeadline(c.BlackTicking, c.Black)
		w.state.Black.Version++
	}
}

func (w *Watcher) StateDelta(old JobCursor) (*JobState, JobCursor, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	d, err := w.state.Delta(old)
	if err != nil {
		return nil, JobCursor{}, fmt.Errorf("delta: %w", err)
	}
	if !w.o.PassRawPV {
		if d.White != nil {
			d.White.PV = nil
		}
		if d.Black != nil {
			d.Black.PV = nil
		}
	}
	return d, w.state.Cursor(), nil
}
