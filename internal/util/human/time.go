package human

import (
	"fmt"
	"math"
	"time"
)

func TimeFromBase(base, t time.Time) string {
	sgnDiff := t.Sub(base)
	neg := sgnDiff < 0
	diff := sgnDiff
	if neg {
		diff = -diff
	}

	if diff < time.Second {
		return "now"
	}

	agoIn := func(s string) string {
		if neg {
			return s + " ago"
		} else {
			return "in " + s
		}
	}

	if diff <= 90*time.Second {
		return agoIn(fmt.Sprintf("%v secs", math.Round(diff.Seconds())))
	}
	if diff <= 90*time.Minute {
		return agoIn(fmt.Sprintf("%v mins", math.Round(diff.Minutes())))
	}
	if diff <= 36*time.Hour {
		return agoIn(fmt.Sprintf("%v hrs", math.Round(diff.Hours())))
	}
	if diff <= 14*24*time.Hour {
		return agoIn(fmt.Sprintf("%v days", math.Round(diff.Hours()/24)))
	}
	return t.Format(time.DateOnly)
}
