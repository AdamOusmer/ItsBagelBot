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

// Writes to the same key inside one window must collapse into the latest
// value: that is the whole reason the database is not hit per modification.
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
	b.Add(3, 3) // hits maxSize, triggers a flush without waiting for the ticker

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

// A failed flush must not lose writes: they stay pending and land on the next
// window, unless a newer write for the same key arrived in between.
func TestFailedFlushRetriesWithoutClobbering(t *testing.T) {
	rec := &recorder{fail: true}

	b := New[string, int](time.Hour, 1, rec.flush, zap.NewNop())

	b.Add("key", 1) // flushes immediately and fails

	// Wait for the first flush to have actually RUN and failed, returning the
	// item to pending. Gating on pending alone is racy: "key" is present from
	// Add before the flush ever takes it, so the wait could fall through before
	// the failing flush runs — then fail=false would take effect and the flush
	// of "1" would succeed, leaving [1, 2] instead of [2].
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

	b.Add("key", 2) // newer write wins over the restored failure

	b.Close(context.Background())

	require.Equal(t, []int{2}, rec.all())
}

// Requeue restores a transiently failed item unless a newer write for the
// same key arrived while the flush ran.
func TestRequeueDoesNotClobberNewerWrite(t *testing.T) {
	rec := &recorder{}

	b := New[string, int](time.Hour, 100, rec.flush, zap.NewNop())

	b.Requeue("gone", 1) // no pending value: restored
	b.Add("fresh", 2)
	b.Requeue("fresh", 1) // newer pending value wins

	b.mu.Lock()
	assert.Equal(t, 1, b.pending["gone"])
	assert.Equal(t, 2, b.pending["fresh"])
	b.mu.Unlock()

	b.Close(context.Background())
}

// The flush deadline exists so a database that accepts connections but never
// answers cannot pin the batcher's single goroutine forever while Add keeps
// accumulating windows it will never drain. The callback must observe a
// context that actually expires.
func TestFlushDeadlineBoundsSlowFlush(t *testing.T) {
	seen := make(chan error, 1)

	// maxSize 1 makes the very first Add kick a flush; the interval ticker
	// stays out of the way at an hour.
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

// Stats must report what alerting needs: pending depth (staleness risk),
// flush/failure counters, and the last window's duration.
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
	// ItemsFlushed counts every window handed to the flush callback, including
	// the failed one that was requeued and retried at Close: 2 + 2.
	assert.GreaterOrEqual(t, stats.ItemsFlushed, uint64(2))
	assert.GreaterOrEqual(t, stats.Flushes, uint64(2))
	assert.Equal(t, uint64(1), stats.Failures)
	assert.NotZero(t, stats.LastDuration)
}

// Concurrent writers to the same key are the production shape (dashboard RPCs
// landing in parallel): every write must survive coalescing as SOME complete
// value, and the window must drain exactly once per key. Run under -race.
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
