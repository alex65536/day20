package scheduler

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/alex65536/day20/internal/opening"
)

type OpeningBookKind string

const (
	OpeningsNone    OpeningBookKind = ""
	OpeningsPGNLine OpeningBookKind = "pgn_line"
	OpeningsFEN     OpeningBookKind = "fen"
	OpeningsBuiltin OpeningBookKind = "builtin"

	BuiltinBookGraham20141F = "graham_2014_1f"
	BuiltinBookGBSelect2020 = "gb_select_2020"
)

type OpeningBook struct {
	Kind OpeningBookKind
	Data string
}

func (b OpeningBook) Book(rnd rand.Source) (opening.Book, error) {
	switch b.Kind {
	case OpeningsPGNLine:
		book, err := opening.NewPGNLineBook(strings.NewReader(b.Data), rnd)
		if err != nil {
			return nil, fmt.Errorf("build pgn line book: %w", err)
		}
		return book, nil
	case OpeningsFEN:
		book, err := opening.NewFENBook(strings.NewReader(b.Data), rnd)
		if err != nil {
			return nil, fmt.Errorf("build fen book: %w", err)
		}
		return book, nil
	case OpeningsBuiltin:
		switch b.Data {
		case BuiltinBookGraham20141F:
			return opening.Graham20141FBook(), nil
		case BuiltinBookGBSelect2020:
			return opening.GBSelect2020Book(), nil
		default:
			return nil, fmt.Errorf("unknown builtin opening book: %q", b.Data)
		}
	default:
		return nil, fmt.Errorf("bad book kind %q", b.Kind)
	}
}
