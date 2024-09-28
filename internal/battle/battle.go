package battle

import (
	"context"
	"fmt"
	"time"

	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"

	"github.com/alex65536/day20/internal/opening"
	"github.com/alex65536/day20/internal/util/clone"
)

type Watcher interface {
	OnGameInited(game *GameExt)
	OnGameUpdated(game *GameExt, clk maybe.Maybe[clock.Clock])
	OnGameFinished(game *GameExt, warn Warnings)
	OnEngineInfo(color chess.Color, status uci.SearchStatus)
}

type Options struct {
	TimeControl maybe.Maybe[clock.Control]
	FixedTime   maybe.Maybe[time.Duration]

	DeadlineMargin   maybe.Maybe[time.Duration]
	MaxWaitGameStart maybe.Maybe[time.Duration]
	MaxWaitStop      maybe.Maybe[time.Duration]
	OutcomeFilter    maybe.Maybe[chess.VerdictFilter]

	// Terminate the game when both sides agree that one of them wins with Score >= ScoreThreshold.
	// Must be set to zero for no threshold.
	ScoreThreshold int32

	EventName string
}

func (o Options) Clone() Options {
	o.TimeControl = clone.Maybe(o.TimeControl)
	return o
}

func (o *Options) FillDefaults() {
	if o.OutcomeFilter.IsNone() {
		o.OutcomeFilter = maybe.Some(chess.VerdictFilterRelaxed)
	}
	if o.DeadlineMargin.IsNone() {
		o.DeadlineMargin = maybe.Some(50 * time.Millisecond)
	}
	if o.MaxWaitGameStart.IsNone() {
		o.MaxWaitGameStart = maybe.Some(3 * time.Second)
	}
	if o.MaxWaitStop.IsNone() {
		o.MaxWaitStop = maybe.Some(5 * time.Millisecond)
	}
	if o.EventName == "" {
		o.EventName = "Day20 Battle"
	}
}

type Battle struct {
	White   EnginePool
	Black   EnginePool
	Book    opening.Book
	Options Options
}

func (b *Battle) pool(c chess.Color) EnginePool {
	if c == chess.ColorWhite {
		return b.White
	} else {
		return b.Black
	}
}

func (b *Battle) doReleaseEngine(p EnginePool, e *uci.Engine) {
	if e.Terminated() {
		return
	}
	defer func() {
		if e != nil {
			e.Close()
		}
	}()
	search := e.CurSearch()
	if search == nil {
		p.ReleaseEngine(e)
		e = nil
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.Options.MaxWaitStop.Get())
	defer cancel()
	if err := search.Stop(ctx, true); err != nil {
		return
	}
	p.ReleaseEngine(e)
	e = nil
}

func (b *Battle) uciNewGame(ctx context.Context, e *uci.Engine) error {
	ctx, cancel := context.WithTimeout(ctx, b.Options.MaxWaitGameStart.Get())
	defer cancel()
	if err := e.UCINewGame(ctx, true); err != nil {
		return fmt.Errorf("ucinewgame: %w", err)
	}
	return nil
}

type Warnings []string

func (b *Battle) predictWin(score maybe.Maybe[uci.Score]) int {
	if score.IsNone() || b.Options.ScoreThreshold == 0 {
		return 0
	}
	sc := score.Get()
	if sc.IsMate() {
		if sc.IsWinMate() {
			return +1
		} else {
			return -1
		}
	} else {
		cp, _ := sc.Centipawns()
		if cp >= b.Options.ScoreThreshold {
			return +1
		}
		if cp <= -b.Options.ScoreThreshold {
			return -1
		}
		return 0
	}
}

func (b *Battle) checkResign(game *clock.Game, scores []maybe.Maybe[uci.Score]) {
	if game.IsFinished() || len(scores) < 2 || b.Options.ScoreThreshold == 0 {
		return
	}
	p1, p2 := b.predictWin(scores[len(scores)-2]), b.predictWin(scores[len(scores)-1])
	side := game.CurSide()
	if p1 > 0 && p2 < 0 {
		_ = game.Finish(chess.MustWinOutcome(chess.VerdictResign, side))
	}
	if p1 < 0 && p2 > 0 {
		_ = game.Finish(chess.MustWinOutcome(chess.VerdictResign, side.Inv()))
	}
}

func (b *Battle) Do(ctx context.Context, watcher Watcher) (*GameExt, Warnings, error) {
	if b.Options.TimeControl.IsSome() && b.Options.FixedTime.IsSome() {
		return nil, nil, fmt.Errorf("conflicting time control")
	}
	if b.Options.TimeControl.IsNone() && b.Options.FixedTime.IsNone() {
		return nil, nil, fmt.Errorf("no time control")
	}
	b.Options.FillDefaults()
	gameExt, warn := b.doImpl(ctx, watcher)
	return gameExt, warn, nil
}

func (b *Battle) doImpl(ctx context.Context, watcher Watcher) (gameExt *GameExt, warn Warnings) {
	opening := b.Book.Opening()
	gameExt = &GameExt{
		Game:        opening,
		Scores:      make([]maybe.Maybe[uci.Score], 0, opening.Len()),
		WhiteName:   b.White.Name(),
		BlackName:   b.Black.Name(),
		Round:       0, // Not specified.
		TimeControl: clone.Maybe(b.Options.TimeControl),
		FixedTime:   b.Options.FixedTime,
		StartTime:   time.Now().Local(),
		Event:       b.Options.EventName,
	}
	for range opening.Len() {
		gameExt.Scores = append(gameExt.Scores, maybe.None[uci.Score]())
	}
	if watcher != nil {
		watcher.OnGameInited(gameExt)
	}
	defer func() {
		if watcher != nil {
			defer watcher.OnGameFinished(gameExt, warn)
		}
	}()

	var engines [chess.ColorMax]*uci.Engine
	defer func() {
		for c, e := range engines {
			if e != nil {
				b.doReleaseEngine(b.pool(chess.Color(c)), e)
			}
		}
	}()
	for c := range chess.ColorMax {
		if err := func() error {
			e, err := b.pool(c).AcquireEngine(ctx)
			if err != nil {
				return fmt.Errorf("acquire: %w", err)
			}
			if err := b.uciNewGame(ctx, e); err != nil {
				e.Close()
				return fmt.Errorf("start game: %w", err)
			}
			engines[c] = e
			return nil
		}(); err != nil {
			warn = append(warn, fmt.Sprintf("engine %q: cannot init: %v", b.pool(c).Name(), err))
			gameExt.Game = opening
			gameExt.Game.SetOutcome(chess.MustWinOutcome(chess.VerdictEngineError, c.Inv()))
			if watcher != nil {
				clk := maybe.None[clock.Clock]()
				if b.Options.TimeControl.IsSome() {
					clk = maybe.Some(clock.Clock{})
				}
				watcher.OnGameUpdated(gameExt, clk)
			}
			return
		}
	}

	game := clock.NewGame(opening, b.Options.TimeControl, clock.GameOptions{
		OutcomeFilter: b.Options.OutcomeFilter,
	})
	gameExt.Game = game.Inner()

	for !game.IsFinished() {
		if watcher != nil {
			watcher.OnGameUpdated(gameExt, maybe.Pack(game.Clock()))
		}
		side := game.CurSide()
		engine := engines[side]
		var deadline time.Time
		if b.Options.TimeControl.IsSome() {
			var ok bool
			deadline, ok = game.Deadline()
			if !ok {
				panic("must not happen")
			}
		} else {
			deadline = time.Now().Add(b.Options.FixedTime.Get())
		}
		deadline = deadline.Add(b.Options.DeadlineMargin.Get())
		if err := func() error {
			ctx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			if err := engine.SetPosition(ctx, game.Inner()); err != nil {
				game.UpdateTimer()
				return fmt.Errorf("set position: %w", err)
			}
			var consumer uci.InfoConsumer
			if watcher != nil {
				consumer = func(search *uci.Search, _ uci.Info) {
					watcher.OnEngineInfo(side, search.Status())
				}
			}
			var search *uci.Search
			search, err := engine.Go(ctx, uci.GoOptions{
				TimeSpec: maybe.Pack(game.UCITimeSpec()),
				Movetime: b.Options.FixedTime,
			}, consumer)
			if err != nil {
				game.UpdateTimer()
				return fmt.Errorf("go: %w", err)
			}
			if err := search.Wait(ctx); err != nil {
				game.UpdateTimer()
				if !game.HasTimer() && !time.Now().Before(deadline) {
					_ = game.Finish(chess.MustWinOutcome(chess.VerdictTimeForfeit, side.Inv()))
				}
				return fmt.Errorf("wait: %w", err)
			}
			mv, err := search.BestMove()
			if err != nil {
				return fmt.Errorf("best move: %w", err)
			}
			if err := game.Push(mv); err != nil {
				return fmt.Errorf("add move: %w", err)
			}
			if game.Inner().Len() != len(gameExt.Scores) {
				gameExt.Scores = append(gameExt.Scores, search.Status().Score)
			}
			b.checkResign(game, gameExt.Scores)
			return nil
		}(); err != nil {
			warn = append(warn, fmt.Sprintf("engine %q: error: %v", b.pool(side).Name(), err))
			if !game.IsFinished() {
				_ = game.Finish(chess.MustWinOutcome(chess.VerdictEngineError, side.Inv()))
			}
			engine.Close()
		}
	}
	if game.Outcome().Verdict() == chess.VerdictTimeForfeit {
		winner, _ := game.Outcome().Status().Winner()
		name := b.pool(winner.Inv()).Name()
		warn = append(warn, fmt.Sprintf("engine %q: forfeits on time", name))
	}

	if watcher != nil {
		watcher.OnGameUpdated(gameExt, maybe.Pack(game.Clock()))
	}
	return
}
