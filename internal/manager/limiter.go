package manager

import (
	"context"
	"sync"
	"time"
)

// Bucket is a token bucket in bytes per second. A zero or negative rate means
// unlimited, and WaitN returns immediately. It is safe for concurrent use.
type Bucket struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

// NewBucket returns a limiter for rate bytes/second. Burst is a quarter second
// of traffic, with a 64 KiB floor.
func NewBucket(rate int64) *Bucket {
	if rate <= 0 {
		return &Bucket{}
	}
	capacity := float64(rate) / 4
	if capacity < 64<<10 {
		capacity = 64 << 10
	}
	return &Bucket{
		rate:     float64(rate),
		capacity: capacity,
		tokens:   capacity,
		last:     time.Now(),
	}
}

// Rate reports the configured bytes per second (0 = unlimited).
func (b *Bucket) Rate() int64 { return int64(b.rate) }

// SetRate changes the limit. Existing waiters observe the new rate.
func (b *Bucket) SetRate(rate int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rate <= 0 {
		b.rate = 0
		b.tokens = 0
		return
	}
	b.rate = float64(rate)
	b.capacity = float64(rate) / 4
	if b.capacity < 64<<10 {
		b.capacity = 64 << 10
	}
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
}

// WaitN blocks until n bytes may be transferred, or ctx is done.
func (b *Bucket) WaitN(ctx context.Context, n int) error {
	if b == nil || b.rate <= 0 || n <= 0 {
		return nil
	}
	for n > 0 {
		take := n
		if float64(take) > b.capacity {
			take = int(b.capacity)
		}
		if err := b.wait(ctx, float64(take)); err != nil {
			return err
		}
		n -= take
	}
	return nil
}

func (b *Bucket) wait(ctx context.Context, need float64) error {
	for {
		b.mu.Lock()
		now := time.Now()
		if b.rate <= 0 {
			b.mu.Unlock()
			return nil
		}
		b.tokens += now.Sub(b.last).Seconds() * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
		if b.tokens >= need {
			b.tokens -= need
			b.mu.Unlock()
			return nil
		}
		missing := need - b.tokens
		b.mu.Unlock()

		d := time.Duration(missing / b.rate * float64(time.Second))
		if d < time.Millisecond {
			d = time.Millisecond
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// multiLimiter applies every limiter in turn, so a read is bounded by both the
// global and the per-download bucket. A nil bucket is skipped by WaitN.
type multiLimiter []*Bucket

func (m multiLimiter) WaitN(ctx context.Context, n int) error {
	for _, b := range m {
		if err := b.WaitN(ctx, n); err != nil {
			return err
		}
	}
	return nil
}
