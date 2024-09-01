package main

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/field"
	"github.com/alex65536/day20/internal/util/style"
)

const maxDuration = time.Duration(math.MaxInt64)

var progressChars = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

func formatProgressBar(size, completed, total int) string {
	var b strings.Builder
	syms := size * 8
	if total != 0 {
		syms = int(math.Round(float64(size*8) * float64(completed) / float64(total)))
	}
	if !style.StdoutSupportsColor() {
		_ = b.WriteByte('[')
	}
	for range size {
		take := min(syms, 8)
		syms -= take
		_, _ = b.WriteRune(progressChars[take])
	}
	if !style.StdoutSupportsColor() {
		_ = b.WriteByte(']')
	}
	return style.WithS(b.String(), 44, 37, 1)
}

func formatDuration(d time.Duration) string {
	if d == maxDuration {
		return "oo"
	}
	d = d.Round(10 * time.Millisecond)
	return d.String()
}

func predictTime(completed, total int, elapsed time.Duration) time.Duration {
	if total == 0 {
		return 0
	}
	if completed == 0 {
		return maxDuration
	}
	nanos := float64(elapsed.Nanoseconds()) / float64(completed) * float64(total)
	if nanos >= float64(math.MaxInt64) {
		return maxDuration
	}
	return time.Duration(int64(nanos)) * time.Nanosecond
}

func formatLOS(los float64) string {
	if math.IsNaN(los) {
		return style.WithS("N/A", 1)
	}
	var color = 33
	if los < 0.1 {
		color = 31
	} else if los > 0.9 {
		color = 32
	}
	return style.WithS(fmt.Sprintf("%.2f", los), 1, color)
}

func formatWinner(f float64, winner field.Winner) string {
	var (
		color int
		bold  bool
		text  string
	)
	switch winner {
	case field.WinnerUnclear:
		color = 33
		text = "Unclear"
	case field.WinnerFirst:
		switch f {
		case 0.90:
			color = 34
		case 0.95:
			color = 32
		case 0.97, 0.99:
			color = 32
			bold = true
		default:
			panic("must not happen")
		}
		text = fmt.Sprintf("First(p=%.2f)", f)
	case field.WinnerSecond:
		switch f {
		case 0.90:
			color = 35
		case 0.95:
			color = 31
		case 0.97, 0.99:
			color = 31
			bold = true
		default:
			panic("must not happen")
		}
		text = fmt.Sprintf("Second(p=%.2f)", f)
	default:
		panic("must not happen")
	}
	styles := []int{color}
	if bold {
		styles = append(styles, 1)
	}
	return style.WithS(text, styles...)
}

func formatEloDiff(d field.EloDiff) string {
	doFmt := func(f float64) string {
		if f == math.Inf(+1) {
			return style.WithS("oo", 33, 1)
		}
		if f == math.Inf(-1) {
			return style.WithS("-oo", 33, 1)
		}
		return style.WithS(fmt.Sprintf("%.2f", f), 1)
	}
	return fmt.Sprintf("%v/%v/%v", doFmt(d.Low), doFmt(d.Avg), doFmt(d.High))
}

type display interface {
	Display(status field.Status, warn battle.Warnings) error
	FinalDisplay(status field.Status) error
}

func makeWatcher(d display) field.Watcher {
	return func(status field.Status, warn battle.Warnings) {
		if err := d.Display(status, warn); err != nil {
			panic(err)
		}
	}
}

type displayImpl struct {
	out   *bufio.Writer
	err   *bufio.Writer
	start time.Time
	total int
	first bool
	quiet bool
	fancy bool
}

func newDisplay(out io.Writer, err io.Writer, total int, quiet bool) display {
	return &displayImpl{
		out:   bufio.NewWriter(out),
		err:   bufio.NewWriter(err),
		start: time.Now(),
		total: total,
		first: true,
		quiet: quiet,
		fancy: style.IsStdoutTTY(),
	}
}

func (d *displayImpl) erase() error {
	if d.first {
		d.first = false
		return nil
	}
	if _, err := d.out.WriteString("\r\033[A\033[2K\033[A\033[2K\033[A\033[2K\033[A\033[2K"); err != nil {
		return fmt.Errorf("erase: %w", err)
	}
	return nil
}

func (d *displayImpl) displayWarn(warn battle.Warnings) error {
	for _, w := range warn {
		if _, err := fmt.Fprintf(d.err, "%v %v\n", style.WithSE("warning:", 33, 1), w); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}
	return nil
}

func (d *displayImpl) displayResult(status field.Status) error {
	if _, err := fmt.Fprintf(
		d.out,
		""+
			"Win: %v, Draw: %v, Lose: %v, Score: %v\n"+
			"LOS: %v, Winner: %v\n"+
			"Elo Diff: %v (low/avg/high, at p = 0.95)\n",
		status.Win,
		status.Draw,
		status.Lose,
		status.ScoreString(),
		formatLOS(status.LOS()),
		formatWinner(status.Winner(0.9, 0.95, 0.97, 0.99)),
		formatEloDiff(status.EloDiff(0.95)),
	); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func (d *displayImpl) displayProgress(status field.Status, fancy bool) error {
	elapsed := time.Since(d.start)
	completed, total := status.Total(), d.total
	ratio := 1.0
	if total != 0 {
		ratio = float64(completed) / float64(total)
	}

	if fancy {
		if _, err := fmt.Fprintf(
			d.out,
			"%v (%.1f%%, %v/%v, %v/%v)\n",
			formatProgressBar(50, completed, total),
			ratio*100.0,
			completed,
			total,
			formatDuration(elapsed),
			formatDuration(predictTime(completed, total, elapsed)),
		); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		if err := d.displayResult(status); err != nil {
			return fmt.Errorf("result: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(
			d.out,
			"Games: %v/%v, Time: %v/%v, Score: %v, Winner: %v\n",
			completed,
			total,
			formatDuration(elapsed),
			formatDuration(predictTime(completed, total, elapsed)),
			status.ScoreString(),
			formatWinner(status.Winner(0.9, 0.95, 0.97, 0.99)),
		); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}

	return nil
}

func (d *displayImpl) Display(status field.Status, warn battle.Warnings) error {
	if d.fancy && !d.quiet {
		if err := d.erase(); err != nil {
			return fmt.Errorf("erase: %w", err)
		}
		if len(warn) != 0 {
			if err := d.out.Flush(); err != nil {
				return fmt.Errorf("flush: %w", err)
			}
		}
	}

	if len(warn) != 0 {
		if err := d.displayWarn(warn); err != nil {
			return fmt.Errorf("warnings: %w", err)
		}
		if err := d.err.Flush(); err != nil {
			return fmt.Errorf("flush: %w", err)
		}
	}

	if d.quiet {
		return nil
	}

	if err := d.displayProgress(status, d.fancy); err != nil {
		return fmt.Errorf("progress: %w", err)
	}
	if err := d.out.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	return nil
}

func (d *displayImpl) FinalDisplay(status field.Status) error {
	if d.fancy && !d.quiet {
		return nil
	}

	if err := d.displayResult(status); err != nil {
		return fmt.Errorf("result: %w", err)
	}
	if err := d.out.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}

	return nil
}
