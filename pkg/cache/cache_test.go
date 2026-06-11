package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The whole point of GetOrLoad is stampede protection: any number of
// concurrent misses on one key must produce exactly one loader call.
func TestGetOrLoadCollapsesConcurrentMisses(t *testing.T) {
	c := New[string](time.Minute)
	defer c.Close()

	var calls atomic.Int32

	loader := func(context.Context) (string, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // hold the flight open so everyone piles up
		return "value", nil
	}

	const goroutines = 64

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			value, err := c.GetOrLoad(context.Background(), "key", loader)
			assert.NoError(t, err)
			assert.Equal(t, "value", value)
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(1), calls.Load(), "concurrent misses must collapse into one load")
}

func TestGetOrLoadCachesValue(t *testing.T) {
	c := New[int](time.Minute)
	defer c.Close()

	var calls atomic.Int32

	loader := func(context.Context) (int, error) {
		calls.Add(1)
		return 42, nil
	}

	for range 10 {
		value, err := c.GetOrLoad(context.Background(), "key", loader)
		require.NoError(t, err)
		require.Equal(t, 42, value)
	}

	assert.Equal(t, int32(1), calls.Load())
}

func TestGetOrLoadDoesNotCacheErrors(t *testing.T) {
	c := New[int](time.Minute)
	defer c.Close()

	boom := errors.New("boom")
	var calls atomic.Int32

	failing := func(context.Context) (int, error) {
		calls.Add(1)
		return 0, boom
	}

	_, err := c.GetOrLoad(context.Background(), "key", failing)
	require.ErrorIs(t, err, boom)

	_, err = c.GetOrLoad(context.Background(), "key", failing)
	require.ErrorIs(t, err, boom)

	assert.Equal(t, int32(2), calls.Load(), "errors must not be cached")
}

func TestEntriesExpire(t *testing.T) {
	c := New[string](20 * time.Millisecond)
	defer c.Close()

	c.Set("key", "value")

	_, ok := c.get("key")
	require.True(t, ok)

	time.Sleep(30 * time.Millisecond) // past TTL plus the maximum jitter

	_, ok = c.get("key")
	assert.False(t, ok, "entry should have expired")
}

func TestInvalidate(t *testing.T) {
	c := New[string](time.Minute)
	defer c.Close()

	c.Set("key", "stale")
	c.Invalidate("key")

	value, err := c.GetOrLoad(context.Background(), "key", func(context.Context) (string, error) {
		return "fresh", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "fresh", value)
}

func TestUserKey(t *testing.T) {
	assert.Equal(t, "user:0", UserKey("user:", 0))
	assert.Equal(t, "modules:123456", UserKey("modules:", 123456))
	assert.Equal(t, "x:18446744073709551615", UserKey("x:", ^uint64(0)))
}

func TestUserKeyAllocatesOnce(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		_ = UserKey("settings:", 123456789)
	})

	assert.LessOrEqual(t, allocs, 1.0)
}
