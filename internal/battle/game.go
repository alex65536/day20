package battle

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
	"github.com/alex65536/go-chess/uci"
	"github.com/alex65536/go-chess/util/maybe"
)

type GameExt struct {
	Game        *chess.Game
	Scores      []maybe.Maybe[uci.Score]
	WhiteName   string
	BlackName   string
	Round       int
	TimeControl maybe.Maybe[clock.Control]
	FixedTime   maybe.Maybe[time.Duration]
	StartTime   time.Time
	Event       string
}

func sgsSanitize(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			_ = b.WriteByte(' ')
			continue
		}
		_ = b.WriteByte(c)
	}
	return b.String()
}

func makePGNTag(name, value string) string {
	var b strings.Builder
	_ = b.WriteByte('[')
	_, _ = b.WriteString(name)
	_, _ = b.WriteString(" \"")
	for i := range len(value) {
		c := value[i]
		if c < 0x20 || c == 0x7f {
			// Turn '\n', '\r', '\t' and other control codes into spaces.
			// Yes, '\t' is explicitly disallowed by the PGN Standard.
			_ = b.WriteByte(' ')
			continue
		}
		if c == '"' || c == '\\' {
			_ = b.WriteByte('\\')
		}
		_ = b.WriteByte(c)
	}
	_, _ = b.WriteString("\"]")
	_ = b.WriteByte('\n')
	return b.String()
}

func invScore(s uci.Score) uci.Score {
	if cp, ok := s.Centipawns(); ok {
		return uci.ScoreCentipawns(-cp)
	}
	if m, ok := s.Mate(); ok {
		return uci.ScoreMate(-m)
	}
	panic("must not happen")
}

func pgnDoWordWrap(b *strings.Builder, s string, maxLineLen int) {
	var words []string
	r := 0
	for {
		for r < len(s) && unicode.IsSpace(rune(s[r])) {
			r++
		}
		if r >= len(s) {
			break
		}
		l := r
		if s[r] == '{' {
			for r < len(s) && s[r] != '}' {
				r++
			}
			if r < len(s) {
				r++
			}
		} else {
			for r < len(s) && !unicode.IsSpace(rune(s[r])) {
				r++
			}
		}
		words = append(words, s[l:r])
	}

	lineLen := 0
	for _, s := range words {
		newLineLen := lineLen + len(s)
		if lineLen != 0 {
			newLineLen++
		}
		if lineLen != 0 && newLineLen > maxLineLen {
			_ = b.WriteByte('\n')
			lineLen = 0
		}
		if lineLen != 0 {
			_ = b.WriteByte(' ')
			lineLen++
		}
		_, _ = b.WriteString(s)
		lineLen += len(s)
	}
	if lineLen != 0 {
		_ = b.WriteByte('\n')
	}
}

func (g *GameExt) PGN() (string, error) {
	var b strings.Builder
	eventStr := g.Event
	if eventStr == "" {
		eventStr = "?"
	}
	dateStr := "????.??.??"
	if !g.StartTime.IsZero() {
		dateStr = g.StartTime.Format(time.DateOnly)
	}
	roundStr := "?"
	if g.Round != 0 {
		roundStr = strconv.FormatInt(int64(g.Round), 10)
	}
	_, _ = b.WriteString(makePGNTag("Event", eventStr))
	_, _ = b.WriteString(makePGNTag("Site", "?"))
	_, _ = b.WriteString(makePGNTag("Date", dateStr))
	_, _ = b.WriteString(makePGNTag("Round", roundStr))
	_, _ = b.WriteString(makePGNTag("White", g.WhiteName))
	_, _ = b.WriteString(makePGNTag("Black", g.BlackName))
	_, _ = b.WriteString(makePGNTag("Result", g.Game.Outcome().Status().String()))
	if g.Game.StartPos() != chess.InitialRawBoard() {
		_, _ = b.WriteString(makePGNTag("SetUp", "1"))
		_, _ = b.WriteString(makePGNTag("FEN", g.Game.StartPos().FEN()))
	}
	if c, ok := g.TimeControl.TryGet(); ok {
		_, _ = b.WriteString(makePGNTag("TimeControl", c.String()))
	}
	if t, ok := g.FixedTime.TryGet(); ok {
		timeStr := (clock.ControlItem{Time: t}).String() // HACK
		_, _ = b.WriteString(makePGNTag("TimePerMove", timeStr))
	}
	switch g.Game.Outcome().Verdict() {
	case chess.VerdictTimeForfeit:
		_, _ = b.WriteString(makePGNTag("Termination", "time forfeit"))
	case chess.VerdictResign:
		_, _ = b.WriteString(makePGNTag("Termination", "adjudication"))
	case chess.VerdictEngineError:
		_, _ = b.WriteString(makePGNTag("Termination", "rules infraction"))
	}
	_ = b.WriteByte('\n')

	glen := g.Game.Len()
	comments := make([][]string, glen+1)
	side := g.Game.StartPos().Side
	for i, maybeSc := range g.Scores {
		if maybeSc.IsSome() {
			sc := maybeSc.Get()
			if side == chess.ColorBlack {
				sc = invScore(sc)
			}
			comments[i+1] = append(comments[i+1], fmt.Sprintf("[%%eval %v]", sc))
		}
		side = side.Inv()
	}
	if g.Game.IsFinished() {
		s := g.Game.Outcome().String()
		s = strings.ToUpper(s[:1]) + s[1:]
		comments[glen] = append(comments[glen], s)
	}

	styled, err := g.Game.StyledExt(chess.GameStyle{
		Move: chess.MoveStyleSAN,
		MoveNumber: chess.MoveNumberStyle{
			Enabled: true,
		},
		Outcome: chess.GameOutcomeShow,
	}, chess.GameAnnotations{
		Comments: comments,
	})
	if err != nil {
		return "", fmt.Errorf("style game: %v", err)
	}

	pgnDoWordWrap(&b, styled, 80)

	return b.String(), nil
}

var statusToSGS = map[chess.Status]rune{
	chess.StatusRunning:   '?',
	chess.StatusDraw:      'D',
	chess.StatusWhiteWins: 'W',
	chess.StatusBlackWins: 'B',
}

func (g *GameExt) SGS() string {
	var b strings.Builder
	winner, ok := statusToSGS[g.Game.Outcome().Status()]
	if !ok {
		panic("must not happen")
	}
	_, _ = fmt.Fprintf(&b, "game %c %v\n", winner, g.Round)
	_, _ = fmt.Fprintf(&b, "title %v vs %v\n", sgsSanitize(g.WhiteName), sgsSanitize(g.BlackName))
	if g.Game.StartPos() == chess.InitialRawBoard() {
		_, _ = b.WriteString("start\n")
	} else {
		_, _ = fmt.Fprintf(&b, "board %v\n", g.Game.StartPos())
	}
	_, _ = fmt.Fprintf(&b, "moves %v\n", g.Game.UCIList())
	return b.String()
}
