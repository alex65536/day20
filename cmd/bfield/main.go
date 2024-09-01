package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/alex65536/go-chess/clock"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/mattn/go-colorable"
	"github.com/spf13/cobra"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/field"
	"github.com/alex65536/day20/internal/opening"
	randutil "github.com/alex65536/day20/internal/util/rand"
	"github.com/alex65536/day20/internal/util/style"
)

var (
	stdout = colorable.NewColorableStdout()
	stderr = colorable.NewColorableStderr()
)

var (
	aJobs           int
	aPGNOut         string
	aSGSOut         string
	aGames          int
	aFixedTimeMsec  int
	aFixedTime      time.Duration
	aControl        string
	aFENBook        string
	aPGNBook        string
	aBuiltinBook    string
	aScoreThreshold int
	aTimeMargin     time.Duration
	aQuiet          bool
)

var cmd = cobra.Command{
	Use:   "bfield engine1 engine2",
	Short: "Runs matches between chess engines",
	Long: `"Clear the battlefield and let me see..."

Battlefield is a tool to run matches between chess engines.
`,
	Version: "0.9.15-beta",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		defer signal.Stop(sig)

		go func() {
			select {
			case <-sig:
				cancel()
			case <-ctx.Done():
			}
			<-sig
			os.Exit(1)
		}()

		if len(args) != 2 {
			return fmt.Errorf("engine names required")
		}
		if aGames <= 0 {
			return fmt.Errorf("non-positive games")
		}
		if aJobs <= 0 {
			return fmt.Errorf("non-positive jobs")
		}
		if aScoreThreshold < 0 {
			return fmt.Errorf("negative score-threshold")
		}
		if aTimeMargin <= 0 {
			return fmt.Errorf("non-positive time-margin")
		}

		o := field.Options{
			Jobs:  aJobs,
			Games: aGames,
			Battle: battle.Options{
				DeadlineMargin: maybe.Some(aTimeMargin),
				ScoreThreshold: int32(aScoreThreshold),
			},
		}

		if cmd.Flags().Lookup("time-msec").Changed {
			if aFixedTimeMsec <= 0 {
				return fmt.Errorf("non-positive time-msec")
			}
			o.Battle.FixedTime = maybe.Some(time.Duration(aFixedTimeMsec) * time.Millisecond)
		} else if cmd.Flags().Lookup("time").Changed {
			if aFixedTime <= 0 {
				return fmt.Errorf("non-positive time")
			}
			o.Battle.FixedTime = maybe.Some(aFixedTime)
		} else if cmd.Flags().Lookup("control").Changed {
			ctrl, err := clock.ControlFromString(aControl)
			if err != nil {
				return fmt.Errorf("bad control: %w", err)
			}
			o.Battle.TimeControl = maybe.Some(ctrl)
		} else {
			return fmt.Errorf("no time control specified (use -t, -T or -c flags)")
		}

		var book opening.Book
		if cmd.Flags().Lookup("fen-book").Changed {
			if err := func() error {
				f, err := os.Open(aFENBook)
				if err != nil {
					return fmt.Errorf("open: %w", err)
				}
				defer f.Close()
				book, err = opening.NewFENBook(f, randutil.DefaultSource())
				if err != nil {
					return fmt.Errorf("parse: %w", err)
				}
				return nil
			}(); err != nil {
				return fmt.Errorf("fen book: %w", err)
			}
		} else if cmd.Flags().Lookup("pgn-book").Changed {
			if err := func() error {
				f, err := os.Open(aPGNBook)
				if err != nil {
					return fmt.Errorf("open: %w", err)
				}
				defer f.Close()
				book, err = opening.NewPGNLineBook(f, randutil.DefaultSource())
				if err != nil {
					return fmt.Errorf("parse: %w", err)
				}
				return nil
			}(); err != nil {
				return fmt.Errorf("pgn book: %w", err)
			}
		} else {
			switch aBuiltinBook {
			case "gb2014":
				book = opening.Graham20141FBook()
			case "gb2020":
				book = opening.GBSelect2020Book()
			default:
				return fmt.Errorf("unknown built-in opening book %q", aBuiltinBook)
			}
		}

		var (
			pgnOut io.Writer
			sgsOut io.Writer
		)
		if cmd.Flags().Lookup("pgn-output").Changed {
			f, err := os.Create(aPGNOut)
			if err != nil {
				return fmt.Errorf("create pgn output: %w", err)
			}
			defer f.Close()
			pgnOut = f
		}
		if cmd.Flags().Lookup("sgs-output").Changed {
			f, err := os.Create(aSGSOut)
			if err != nil {
				return fmt.Errorf("create sgs output: %w", err)
			}
			defer f.Close()
			sgsOut = f
		}

		first, err := battle.NewEnginePool(ctx, battle.EnginePoolOptions{Name: args[0]})
		if err != nil {
			return fmt.Errorf("init first engine: %w", err)
		}
		defer first.Close()
		second, err := battle.NewEnginePool(ctx, battle.EnginePoolOptions{Name: args[1]})
		if err != nil {
			return fmt.Errorf("init second engine: %w", err)
		}
		defer second.Close()

		display := newDisplay(stdout, stderr, o.Games, aQuiet)
		c := field.Config{
			Writer: field.WriterConfig{
				PGN: pgnOut,
				SGS: sgsOut,
			},
			Book:    book,
			First:   first,
			Second:  second,
			Watcher: makeWatcher(display),
		}
		status, err := field.Fight(ctx, o, c)
		if err := display.FinalDisplay(status); err != nil {
			panic(err)
		}
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "%vfatal error%v: %v", style.SE(31, 1), style.SE(), err)
			}
		}

		return nil
	},
}

func main() {
	cmd.SetOutput(stdout)
	cmd.SetErr(stderr)
	cmd.SetErrPrefix(style.WithSE("error:", 31, 1))
	cmd.SetHelpTemplate(`{{.Long | trimTrailingWhitespaces}}

{{.UsageString}}
Extra Help:

` + style.WithS("Time Control Format", 4) + `

  Time control format must consist of one or more stages separated by ":". Each
stage must have one of the following formats: T, M/T, T+I or M/T+I, where M is
the number of moves in the stage, T is the amount of time in seconds given for
the stage, and I is the increment in seconds per each move. Note that the last
stage is repeated. You can also specify different time control for the first
and the second engine. To do this, you must separate time control for white and
black with "|".

  For example, "40/900+5:900+5" means 15 minutes for 40 moves plus 5 seconds
each move. After 40 moves pass, you are given 15 minutes for the rest of the
game plus 5 seconds each move. And "300|240" means 5 minutes per game for
white, and 4 minutes per game for black.

` + style.WithS("SoFGameSet Format", 4) + `

  To learn about SoFGameSet format, see the following specification:

  https://github.com/alex65536/sofcheck/blob/master/docs/gameset.md
`)
	cmd.Flags().IntVarP(
		&aJobs, "jobs", "j", max(1, runtime.NumCPU()-2),
		"number of games to run simultaneoulsly")
	cmd.Flags().StringVarP(
		&aPGNOut, "pgn-output", "o", "",
		"file where to write games in PGN format")
	cmd.Flags().StringVarP(
		&aSGSOut, "sgs-output", "r", "",
		"file where to write games in SoFGameSet format\n(see also \"SoFGameSet Format\" section in extra help)")
	cmd.Flags().IntVarP(
		&aGames, "games", "g", 0,
		"number of games to run",
	)
	if err := cmd.MarkFlagRequired("games"); err != nil {
		panic(err)
	}
	cmd.Flags().IntVarP(
		&aFixedTimeMsec, "time-msec", "t", 0,
		"run engines on fixed time (in milliseconds)",
	)
	cmd.Flags().DurationVarP(
		&aFixedTime, "time", "T", 0,
		"run engines on fixed time",
	)
	cmd.Flags().StringVarP(
		&aControl, "control", "c", "",
		"run engines on given time control\n(see also \"Time Control Format\" section in extra help)",
	)
	cmd.MarkFlagsMutuallyExclusive("time", "time-msec", "control")
	cmd.Flags().StringVarP(
		&aFENBook, "fen-book", "f", "",
		"start games from FENs found in the file",
	)
	cmd.Flags().StringVarP(
		&aPGNBook, "pgn-book", "p", "",
		"start games from PGNs found in the file",
	)
	cmd.Flags().StringVarP(
		&aBuiltinBook, "builtin-book", "b", "gb2020",
		"start games using a built-in opening book\n(available: \"gb2020\", \"gb2014\")",
	)
	cmd.Flags().IntVarP(
		&aScoreThreshold, "score-threshold", "s", 0,
		"end the game when both sides agree that the score is larger than the threshold (in centipawns)",
	)
	cmd.Flags().DurationVarP(
		&aTimeMargin, "time-margin", "M", 20*time.Millisecond,
		"extra time to think after deadline\n(increase this if your engine times out in fixed-time mode)",
	)
	cmd.Flags().BoolVarP(
		&aQuiet, "quiet", "q", false,
		"do not report progress",
	)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
