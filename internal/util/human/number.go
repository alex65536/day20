package human

import (
	"strconv"
	"strings"
)

var numSuffixes = []string{"K", "M", "G", "T", "P", "E"}

func Int(n int64, prec int) string {
	if n < 0 {
		return "-" + Uint(-uint64(n), prec)
	}
	return Uint(uint64(n), prec)
}

func Uint(n uint64, prec int) string {
	if n < 1000 {
		return strconv.FormatUint(n, 10)
	}
	mul, rem := uint64(1), n
	for _, suf := range numSuffixes {
		mul *= 1000
		rem /= 1000
		if rem >= 1000 {
			continue
		}
		var b strings.Builder
		_, _ = b.WriteString(strconv.FormatUint(rem, 10))
		maxFrac := prec - b.Len()
		frac, fracMul := 0, mul
		for frac < maxFrac && fracMul > 1 {
			frac++
			fracMul /= 10
		}
		fracRem := (n - rem*mul) / fracMul
		for frac > 0 && fracRem % 10 == 0 {
			frac--
			fracRem /= 10
		}
		if frac != 0 {
			_ = b.WriteByte('.')
			s := strconv.FormatUint(fracRem, 10)
			for range frac - len(s) {
				_ = b.WriteByte('0')
			}
			_, _ = b.WriteString(s)
		}
		_, _ = b.WriteString(suf)
		return b.String()
	}
	panic("must not happen")
}
