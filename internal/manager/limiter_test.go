package manager

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBucketUnlimitedNeverBlocks(t *testing.T) {
	b := NewBucket(0)
	start := time.Now()
	if err := b.WaitN(context.Background(), 64<<20); err != nil {
		t.Fatalf("WaitN: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("unlimited bucket waited %v", elapsed)
	}
}

func TestBucketDelaysLargeRequests(t *testing.T) {
	const rate = 1 << 20 // 1 MiB/s
	b := NewBucket(rate)
	// Consume the initial burst, then ask for a quarter second of traffic.
	if err := b.WaitN(context.Background(), 1<<20); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := b.WaitN(context.Background(), 256<<10); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("256 KiB at 1 MiB/s took %v, want ~250ms", elapsed)
	}
}

func TestBucketWaitIsCancellable(t *testing.T) {
	b := NewBucket(1024) // 1 KiB/s
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := b.WaitN(ctx, 16<<20)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestBucketSetRateZeroUnlimits(t *testing.T) {
	b := NewBucket(1024)
	b.SetRate(0)
	start := time.Now()
	if err := b.WaitN(context.Background(), 64<<20); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("bucket still throttling after SetRate(0): %v", elapsed)
	}
}

func TestMultiLimiterSkipsNilBuckets(t *testing.T) {
	limited := NewBucket(1 << 20)
	ml := multiLimiter{nil, limited, nil}
	if err := ml.WaitN(context.Background(), 1<<10); err != nil {
		t.Fatalf("WaitN: %v", err)
	}
	if got := limited.Rate(); got != 1<<20 {
		t.Fatalf("rate = %d", got)
	}
}
