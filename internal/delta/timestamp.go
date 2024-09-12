package delta

import (
	"math/rand/v2"
	"time"
)

type Timestamp time.Duration

// Randomize timestamp base to catch subtle timing bugs.
var timestampBase = time.Now().Add(-12*time.Hour - time.Duration(rand.Int64N(int64(12*time.Hour))))

func TimestampBase() time.Time {
	return timestampBase
}

func NowTimestamp() Timestamp {
	return Timestamp(time.Now().Sub(timestampBase))
}

type TimestampDiff struct {
	TheirNow Timestamp
	OurNow   Timestamp
}

func FixTimestamp(diff TimestampDiff, theirTs Timestamp) (ourTs Timestamp) {
	ourTs = theirTs - diff.TheirNow + diff.OurNow
	return
}

func (t Timestamp) Add(d time.Duration) Timestamp {
	return t + Timestamp(d)
}

func (t Timestamp) Sub(u Timestamp) time.Duration {
	return time.Duration(t - u)
}

func (t Timestamp) ToTime() time.Time {
	return timestampBase.Add(time.Duration(t))
}
