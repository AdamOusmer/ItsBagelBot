// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package batch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

type recorder struct {
	mu       sync.Mutex
	flushes  [][]int
	attempts int
	fail     bool
}

func (r *recorder) flush(_ context.Context, items []int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attempts++
	if r.fail {
		return errors.New("flush failed")
	}

	r.flushes = append(r.flushes, append([]int(nil), items...))
	return nil
}

func (r *recorder) attemptCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts
}

func (r *recorder) all() []int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []int
	for _, f := range r.flushes {
		out = append(out, f...)
	}
	return out
}

func TestCoalescesSameKey(t *testing.T) {
	rec := &recorder{}

	b := New[string, int](time.Hour, 100, rec.flush, zap.NewNop())

	b.Add("key", 1)
	b.Add("key", 2)
	b.Add("key", 3)

	b.Close(context.Background())

	require.Equal(t, []int{3}, rec.all(), "only the last write per key may survive the window")
}

func TestFlushesWhenFull(t *testing.T) {
	rec := &recorder{}

	b := New[int, int](time.Hour, 3, rec.flush, zap.NewNop())

	b.Add(1, 1)
	b.Add(2, 2)
	b.Add(3, 3)

	assert.Eventually(t, func() bool {
		return len(rec.all()) == 3
	}, time.Second, 5*time.Millisecond)

	b.Close(context.Background())
}

func TestFlushesOnInterval(t *testing.T) {
	rec := &recorder{}

	b := New[string, int](20*time.Millisecond, 100, rec.flush, zap.NewNop())

	b.Add("key", 7)

	assert.Eventually(t, func() bool {
		return len(rec.all()) == 1
	}, time.Second, 5*time.Millisecond)

	b.Close(context.Background())
}

func TestCloseFlushesPending(t *testing.T) {
	rec := &recorder{}

	b := New[string, int](time.Hour, 100, rec.flush, zap.NewNop())

	b.Add("a", 1)
	b.Add("b", 2)

	b.Close(context.Background())

	assert.ElementsMatch(t, []int{1, 2}, rec.all())
}

func TestFailedFlushRetriesWithoutClobbering(t *testing.T) {
	rec := &recorder{fail: true}

	b := New[string, int](time.Hour, 1, rec.flush, zap.NewNop())

	b.Add("key", 1)

	assert.Eventually(t, func() bool {
		if rec.attemptCount() < 1 {
			return false
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		_, pending := b.pending["key"]
		return pending
	}, time.Second, 5*time.Millisecond, "failed item must return to pending")

	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()

	b.Add("key", 2)

	b.Close(context.Background())

	require.Equal(t, []int{2}, rec.all())
}

func TestRequeueDoesNotClobberNewerWrite(t *testing.T) {
	rec := &recorder{}

	b := New[string, int](time.Hour, 100, rec.flush, zap.NewNop())

	b.Requeue("gone", 1)
	b.Add("fresh", 2)
	b.Requeue("fresh", 1)

	b.mu.Lock()
	assert.Equal(t, 1, b.pending["gone"])
	assert.Equal(t, 2, b.pending["fresh"])
	b.mu.Unlock()

	b.Close(context.Background())
}

func TestFlushDeadlineBoundsSlowFlush(t *testing.T) {
	seen := make(chan error, 1)

	b := New[string, int](time.Hour, 1, func(ctx context.Context, _ []int) error {
		<-ctx.Done()
		seen <- ctx.Err()
		return nil
	}, zap.NewNop())
	b.deadline = 20 * time.Millisecond

	b.Add("key", 1)

	select {
	case err := <-seen:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("flush was never bounded by the deadline")
	}

	b.Close(context.Background())
}

func TestStatsTrackWindows(t *testing.T) {
	rec := &recorder{fail: true}

	b := New[string, int](20*time.Millisecond, 4, rec.flush, zap.NewNop())

	b.Add("a", 1)
	b.Add("b", 2)

	assert.Eventually(t, func() bool {
		return b.Stats().Failures >= 1
	}, time.Second, 5*time.Millisecond, "the failed window must count")

	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()

	b.Close(context.Background())

	stats := b.Stats()
	assert.Zero(t, stats.Pending, "Close must drain pending")
	assert.GreaterOrEqual(t, stats.ItemsFlushed, uint64(2))
	assert.GreaterOrEqual(t, stats.Flushes, uint64(2))
	assert.Equal(t, uint64(1), stats.Failures)
	assert.NotZero(t, stats.LastDuration)
}

func TestConcurrentAddsCoalesce(t *testing.T) {
	rec := &recorder{}

	b := New[int, int](10*time.Millisecond, 1024, rec.flush, zap.NewNop())

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				b.Add(1, w*1000+i)
			}
		}(w)
	}
	wg.Wait()

	b.Close(context.Background())

	flushes := rec.all()
	require.NotEmpty(t, flushes, "pending writes must have been flushed")
	assert.LessOrEqual(t, len(flushes), 2, "1600 writes to one key may not become a flush per write")

	for _, v := range flushes {
		assert.GreaterOrEqual(t, v, 0)
	}
}

func TestCloseRetriesFailedFinalDrain(t *testing.T) {
	rec := &recorder{fail: true}

	b := New[string, int](time.Hour, 1, rec.flush, zap.NewNop())

	b.Add("key", 1)

	assert.Eventually(t, func() bool {
		return b.pendingCount() == 1 && rec.attemptCount() == 1
	}, time.Second, 5*time.Millisecond, "the failing window must be back in pending")

	rec.mu.Lock()
	rec.fail = false
	rec.mu.Unlock()

	b.Close(context.Background())

	require.Equal(t, []int{1}, rec.all(), "Close must retry the final drain to success")
	assert.Zero(t, b.Stats().Pending)
}

func TestCloseExpiryGivesUpAfterRetries(t *testing.T) {
	rec := &recorder{fail: true}

	b := New[string, int](time.Hour, 100, rec.flush, zap.NewNop())
	b.Add("key", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	started := time.Now()
	b.Close(ctx)

	assert.GreaterOrEqual(t, rec.attemptCount(), 2, "Close must retry, not drain once and quit")
	assert.Less(t, time.Since(started), 5*time.Second, "an expired budget must end the drain promptly")
	assert.Equal(t, 1, b.pendingCount(), "undrained writes stay visible for the loss log")
}
