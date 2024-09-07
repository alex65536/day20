package field

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/alex65536/day20/internal/battle"
)

type WriterOptions struct {
	NoFlushAfterWrite bool
}

type WriterConfig struct {
	PGN  io.Writer
	SGS  io.Writer
	Opts WriterOptions
}

type Writer struct {
	pgn   *bufio.Writer
	sgs   *bufio.Writer
	errs  []error
	first bool
	opts  WriterOptions
}

func NewWriter(c WriterConfig) *Writer {
	w := &Writer{first: true, opts: c.Opts}
	if c.PGN != nil {
		w.pgn = bufio.NewWriter(c.PGN)
	}
	if c.SGS != nil {
		w.sgs = bufio.NewWriter(c.SGS)
	}
	return w
}

func (w *Writer) flush(b *bufio.Writer, name string) *bufio.Writer {
	if b != nil {
		if err := b.Flush(); err != nil {
			w.errs = append(w.errs, fmt.Errorf("flush %v: %w", name, err))
			return nil
		}
	}
	return b
}

func (w *Writer) WriteGame(g *battle.GameExt) {
	first := w.first
	w.first = false
	if w.pgn != nil {
		if err := func() error {
			s, err := g.PGN()
			if err != nil {
				return fmt.Errorf("convert pgn: %w", err)
			}
			if !first {
				if err := w.pgn.WriteByte('\n'); err != nil {
					return fmt.Errorf("write pgn: %w", err)
				}
			}
			if _, err := w.pgn.WriteString(s); err != nil {
				return fmt.Errorf("write pgn: %w", err)
			}
			return nil
		}(); err != nil {
			w.errs = append(w.errs, err)
			w.flush(w.pgn, "pgn")
			w.pgn = nil
		}
		if !w.opts.NoFlushAfterWrite {
			w.pgn = w.flush(w.pgn, "pgn")
		}
	}
	if w.sgs != nil {
		if err := func() error {
			s := g.SGS()
			if !first {
				if err := w.sgs.WriteByte('\n'); err != nil {
					return fmt.Errorf("write sgs: %w", err)
				}
			}
			if _, err := w.sgs.WriteString(s); err != nil {
				return fmt.Errorf("write sgs: %w", err)
			}
			return nil
		}(); err != nil {
			w.errs = append(w.errs, err)
			w.flush(w.sgs, "sgs")
			w.sgs = nil
		}
		if !w.opts.NoFlushAfterWrite {
			w.sgs = w.flush(w.sgs, "sgs")
		}
	}
}

func (w *Writer) Finish() error {
	w.flush(w.pgn, "pgn")
	w.pgn = nil
	w.flush(w.sgs, "sgs")
	w.sgs = nil
	return errors.Join(w.errs...)
}
