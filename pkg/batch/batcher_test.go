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
	mu      sync.Mutex
	flushes [][]int
	fail    bool
}

func (r *recorder) flush(_ context.Context, items []int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail {
		return errors.New("flush failed")
	}

	r.flushes = append(r.flushes, append([]int(nil), items...))
	return nil
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

	assert.Eventually(t, func() bool {
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
