package field

import (
	"fmt"
	"math"
	"slices"
	"strconv"
)

type Winner int8

const (
	WinnerSecond  Winner = -1
	WinnerUnclear Winner = 0
	WinnerFirst   Winner = +1
)

type EloDiff struct {
	Low  float64
	Avg  float64
	High float64
}

type Status struct {
	Win  int
	Draw int
	Lose int
}

func (s Status) WinRate() float64 {
	return float64(2*s.Win+s.Draw) / float64(2*s.Total())
}

func (s Status) WinRateStdDev() float64 {
	if s.Total() <= 5 {
		return 1.0
	}
	total := float64(s.Total())
	mu := s.WinRate()
	d := mu*(1.0-mu) - float64(s.Draw)/(4.0*total)
	if d <= 0.0 {
		d = 0.0
	}
	return math.Sqrt(d) / math.Sqrt(total)
}

func (s Status) Total() int {
	return s.Win + s.Draw + s.Lose
}

func (s Status) ScoreString() string {
	doFmt := func(i int) string {
		if i%2 == 0 {
			return strconv.FormatInt(int64(i/2), 10) + ".0"
		} else {
			return strconv.FormatInt(int64(i/2), 10) + ".5"
		}
	}
	return fmt.Sprintf("%v:%v", doFmt(s.Win*2+s.Draw), doFmt(s.Lose*2+s.Draw))
}

func (s Status) LOS() float64 {
	if s.Win+s.Lose == 0 {
		return math.NaN()
	}
	return 0.5 * (1.0 + math.Erf(float64(s.Win-s.Lose)/math.Sqrt(2.0*float64(s.Win+s.Lose))))
}

func confidence(p float64) float64 {
	return math.Sqrt2 * math.Erfinv(p)
}

func (s Status) EloDiff(p float64) EloDiff {
	if s.Total() == 0 {
		return EloDiff{
			Low:  math.Inf(-1),
			Avg:  0,
			High: math.Inf(+1),
		}
	}
	mu := s.WinRate()
	delta := s.WinRateStdDev() * confidence(p)
	return EloDiff{
		Low:  EloDifferenceFromRate(mu - delta),
		Avg:  EloDifferenceFromRate(mu),
		High: EloDifferenceFromRate(mu + delta),
	}
}

func EloDifferenceFromRate(winRate float64) float64 {
	const eps = 1e-12
	switch {
	case winRate >= 1.0-eps:
		return math.Inf(+1)
	case winRate <= eps:
		return math.Inf(-1)
	default:
		return -math.Log10(1.0/winRate-1.0) * 400.0
	}
}

func (s Status) Winner(ps ...float64) (float64, Winner) {
	slices.Sort(ps)
	slices.Reverse(ps)
	mu := s.WinRate()
	sigma := s.WinRateStdDev()
	for _, p := range ps {
		c := confidence(p)
		if mu-c*sigma > 0.5 {
			return p, WinnerFirst
		}
		if mu+c*sigma < 0.5 {
			return p, WinnerSecond
		}
	}
	return 0.0, WinnerUnclear
}
