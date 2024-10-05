package opening

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"strings"

	"github.com/alex65536/go-chess/chess"

	"github.com/alex65536/day20/internal/util/randutil"
)

var moveNumRegex = regexp.MustCompile(`^[0-9]+\.$`)

type Book interface {
	Opening() *chess.Game
}

var (
	_ Book = (*emptyBook)(nil)
	_ Book = (*fenBook)(nil)
	_ Book = (*pgnLineBook)(nil)
	_ Book = (*singleBook)(nil)
)

type emptyBook struct{}

func (*emptyBook) Opening() *chess.Game {
	return chess.NewGame()
}

func NewEmptyBook() Book {
	return &emptyBook{}
}

type fenBook struct {
	boards []*chess.Board
	rnd    *rand.Rand
}

func (b *fenBook) Opening() *chess.Game {
	board := b.boards[b.rnd.IntN(len(b.boards))]
	return chess.NewGameWithPosition(board)
}

func NewFENBook(r io.Reader, source rand.Source) (Book, error) {
	var boards []*chess.Board
	br := bufio.NewReader(r)
	lineNo := 0
	for {
		lineNo++
		ln, err := br.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("read: %w", err)
			}
			if ln == "" {
				break
			}
		}
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		b, err := chess.BoardFromFEN(ln)
		if err != nil {
			return nil, fmt.Errorf("line %d: parse board: %w", lineNo, err)
		}
		boards = append(boards, b)
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("no boards in opening book")
	}
	return &fenBook{
		boards: boards,
		rnd:    rand.New(randutil.NewConcurrentSource(source)),
	}, nil
}

type singleBook struct {
	game *chess.Game
}

func (b *singleBook) Opening() *chess.Game {
	return b.game.Clone()
}

func NewSingleGameBook(game *chess.Game) Book {
	return &singleBook{game: game.Clone()}
}

type pgnLineBook struct {
	games []*chess.Game
	rnd   *rand.Rand
}

func (b *pgnLineBook) Opening() *chess.Game {
	return b.games[b.rnd.IntN(len(b.games))].Clone()
}

func NewPGNLineBook(r io.Reader, source rand.Source) (Book, error) {
	var games []*chess.Game
	br := bufio.NewReader(r)
	lineNo := 0
	for {
		lineNo++
		ln, err := br.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("read: %w", err)
			}
			if ln == "" {
				break
			}
		}
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		g := chess.NewGame()
		moveNo := 0
		for _, tok := range strings.Fields(ln) {
			if moveNumRegex.MatchString(tok) {
				continue
			}
			moveNo++
			if err := g.PushMoveSAN(tok); err != nil {
				return nil, fmt.Errorf("line %d: parse move %d: %w", lineNo, moveNo, err)
			}
		}
		games = append(games, g)
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("no games in opening book")
	}
	return &pgnLineBook{
		games: games,
		rnd:   rand.New(randutil.NewConcurrentSource(source)),
	}, nil
}

func builtinPGNLineBook(s string) Book {
	b, err := NewPGNLineBook(strings.NewReader(s), randutil.DefaultSource())
	if err != nil {
		panic(err)
	}
	return b
}

//go:embed data/Graham2014-1F.txt
var graham20141F string

//go:embed data/GBSelect2020.txt
var gbSelect2020 string

var (
	graham20141FBook = builtinPGNLineBook(graham20141F)
	gbSelect2020Book = builtinPGNLineBook(gbSelect2020)
)

// Graham2014-1F.cgb opening book by Graham Banks <gbanksnz at gmail.com>.
// Source URL: https://www.talkchess.com/forum3/viewtopic.php?t=50541#p549216
func Graham20141FBook() Book {
	return graham20141FBook
}

// GBSelect2020.pgn opening book by Graham Banks <gbanksnz at gmail.com>.
func GBSelect2020Book() Book {
	return gbSelect2020Book
}
