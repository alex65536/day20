package field

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/util/maybe"
	"golang.org/x/sync/errgroup"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/opening"
	"github.com/alex65536/day20/internal/stat"
)

type Options struct {
	Jobs   int
	Games  int
	Battle battle.Options
}

type Watcher func(s stat.Status, warn battle.Warnings)

type Config struct {
	Writer  WriterConfig
	Book    opening.Book
	First   battle.EnginePool
	Second  battle.EnginePool
	Watcher Watcher
}

func Fight(ctx context.Context, o Options, c Config) (stat.Status, error) {
	eg, gctx := errgroup.WithContext(ctx)
	eg.SetLimit(o.Jobs)

	type output struct {
		game   *battle.GameExt
		warn   battle.Warnings
		invert bool
	}

	outputs := make(chan output, 1)
	launched := make(chan struct{})
	go func() {
		defer close(launched)
		for i := range o.Games {
			select {
			case <-gctx.Done():
				return
			default:
			}
			invert := i%2 == 1
			eg.Go(func() error {
				battle := battle.Battle{
					White:   c.First,
					Black:   c.Second,
					Book:    c.Book,
					Options: o.Battle.Clone(),
				}
				if invert {
					battle.White, battle.Black = battle.Black, battle.White
					if ctrl, ok := battle.Options.TimeControl.TryGet(); ok {
						ctrl.White, ctrl.Black = ctrl.Black, ctrl.White
						battle.Options.TimeControl = maybe.Some(ctrl)
					}
				}
				game, warn, err := battle.Do(gctx, nil)
				if err != nil {
					return fmt.Errorf("battle: %w", err)
				}
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}
				select {
				case outputs <- output{game: game, warn: warn, invert: invert}:
				case <-gctx.Done():
					return gctx.Err()
				}
				return nil
			})
		}
	}()

	writer := NewWriter(c.Writer)
	status := stat.Status{Win: 0, Draw: 0, Lose: 0}
	c.Watcher(status, nil)
	for i := range o.Games {
		select {
		case out := <-outputs:
			out.game.Round = i + 1
			switch out.game.Game.Outcome().Status() {
			case chess.StatusWhiteWins:
				if out.invert {
					status.Lose++
				} else {
					status.Win++
				}
			case chess.StatusBlackWins:
				if out.invert {
					status.Win++
				} else {
					status.Lose++
				}
			case chess.StatusDraw:
				status.Draw++
			default:
				panic("must not happen")
			}
			c.Watcher(status, out.warn)
			writer.WriteGame(out.game)
		case <-gctx.Done():
			break
		}
	}
	wErr := writer.Finish()
	if wErr != nil {
		wErr = fmt.Errorf("writer: %w", wErr)
	}

	<-launched
	if err := eg.Wait(); err != nil {
		return status, errors.Join(fmt.Errorf("wait: %w", err), wErr)
	}
	return status, wErr
}
