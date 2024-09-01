package style

import (
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
)

var (
	// Respect https://no-color.org/.
	noColor = os.Getenv("NO_COLOR") != ""

	isTTY      = isatty.IsTerminal(os.Stdout.Fd())
	isColor    = isTTY && !noColor
	isErrTTY   = isatty.IsTerminal(os.Stderr.Fd())
	isErrColor = isErrTTY && !noColor
)

func IsStdoutTTY() bool         { return isTTY }
func IsStderrTTY() bool         { return isErrTTY }
func StdoutSupportsColor() bool { return isColor }
func StderrSupportsColor() bool { return isErrColor }

func doS(ms []int) string {
	if len(ms) == 0 {
		return "\033[0m"
	}
	var b strings.Builder
	_, _ = b.WriteString("\033[")
	for i, m := range ms {
		if i != 0 {
			_ = b.WriteByte(';')
		}
		_, _ = b.WriteString(strconv.FormatInt(int64(m), 10))
	}
	_ = b.WriteByte('m')
	return b.String()
}

func S(ms ...int) string {
	if isColor {
		return doS(ms)
	}
	return ""
}

func SE(ms ...int) string {
	if isErrColor {
		return doS(ms)
	}
	return ""
}

func WithS(s string, ms ...int) string  { return S(ms...) + s + S() }
func WithSE(s string, ms ...int) string { return SE(ms...) + s + SE() }
