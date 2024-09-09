package backoff

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

type Options struct {
	// Must be positive. Zero means default.
	Min time.Duration
	// Must be positive. Zero means default.
	Max time.Duration
	// Must be >= 1.0. Zero means default.
	Grow float64
	// Must be >= 1.0. Zero means default.
	Jitter float64
	// Zero means default, negative means unlimited.
	MaxAttempts int64
}

func (o *Options) Validate() error {
	if o.Min < 0 {
		return fmt.Errorf("negative min")
	}
	if o.Max < 0 {
		return fmt.Errorf("negative max")
	}
	if o.Grow < 1.0 && o.Grow != 0.0 {
		return fmt.Errorf("grow < 1.0")
	}
	if o.Jitter < 1.0 && o.Jitter != 0.0 {
		return fmt.Errorf("jitter < 1.0")
	}
	return nil
}

func (o *Options) FillDefaults() {
	if o.Min == 0 {
		o.Min = 500 * time.Millisecond
	}
	if o.Max == 0 {
		o.Max = time.Minute
	}
	if o.Grow == 0.0 {
		o.Grow = 2.0
	}
	if o.Jitter == 0.0 {
		o.Jitter = 1.5
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 64
	}
}

type Backoff struct {
	o    Options
	cur  time.Duration
	left int64
}

func New(o Options) (*Backoff, error) {
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("bad config: %w", err)
	}
	o.FillDefaults()
	b := &Backoff{o: o}
	b.Reset()
	return b, nil
}

func (b *Backoff) Reset() {
	b.cur = b.o.Min
	b.left = b.o.MaxAttempts
}

func (b *Backoff) Next() (time.Duration, bool) {
	if b.left > 0 {
		b.left--
	}
	if b.left == 0 {
		return 0, false
	}
	flMax := float64(b.o.Max.Nanoseconds())
	newTime := min(flMax, float64(b.cur.Nanoseconds())*b.o.Grow)
	jitter := 1.0 + rand.Float64()*(b.o.Jitter-1.0)
	waitTime := min(flMax, newTime*jitter)
	b.cur = time.Duration(int64(newTime))
	return time.Duration(int64(waitTime)), true
}

func (b *Backoff) Retry(ctx context.Context, err error) error {
	t, ok := b.Next()
	if !ok {
		return fmt.Errorf("retry limit exceeded: %w", err)
	}
	select {
	case <-time.After(t):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
