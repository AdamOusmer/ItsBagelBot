// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uintKey(id uint64) string { return strconv.FormatUint(id, 10) }

func TestGetOrLoadCollapsesConcurrentMisses(t *testing.T) {
	c := cache.NewKeyed[uint64, bool](cache.DefaultCapacity, time.Minute, uintKey)
	defer c.Close()

	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	loader := func(context.Context) (bool, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return true, nil
	}

	const goroutines = 64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			value, err := c.GetOrLoad(context.Background(), 7, loader)
			assert.NoError(t, err)
			assert.True(t, value)
		})
	}
	<-entered
	close(release)
	wg.Wait()

	assert.Equal(t, int64(1), calls.Load(), "concurrent misses must collapse into one load")
}

func TestGetOrLoadCachesValuesButNotErrors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		loadErr   error
		wantLoads int32
	}{
		{name: "caches a loaded value", wantLoads: 1},
		{name: "does not cache errors", loadErr: boom, wantLoads: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cache.New[int](1000, time.Minute)
			defer c.Close()
			var calls atomic.Int32
			loader := func(context.Context) (int, error) {
				calls.Add(1)
				return 42, tc.loadErr
			}

			for range 3 {
				value, err := c.GetOrLoad(context.Background(), "key", loader)
				require.ErrorIs(t, err, tc.loadErr)
				if tc.loadErr == nil {
					require.Equal(t, 42, value)
				}
			}

			assert.Equal(t, tc.wantLoads, calls.Load())
		})
	}
}

func TestGetOrLoadTTLUsesLoaderTTL(t *testing.T) {
	c := cache.New[string](1000, time.Minute)
	defer c.Close()

	var calls atomic.Int32
	loader := func(context.Context) (string, time.Duration, error) {
		calls.Add(1)
		return "value", 20 * time.Millisecond, nil
	}

	for range 2 {
		value, err := c.GetOrLoadTTL(context.Background(), "key", loader)
		require.NoError(t, err)
		require.Equal(t, "value", value)
	}
	require.Equal(t, int32(1), calls.Load())

	require.Eventually(t, func() bool {
		_, err := c.GetOrLoadTTL(context.Background(), "key", loader)
		return err == nil && calls.Load() == 2
	}, time.Second, 10*time.Millisecond, "entry should expire after the loader TTL")
}

func TestGetReportsHitsMissesAndZeroValues(t *testing.T) {
	c := cache.NewKeyed[uint64, bool](cache.DefaultCapacity, time.Minute, uintKey)
	defer c.Close()

	_, ok := c.Get(1)
	require.False(t, ok, "missing entry must not be reported as cached")

	c.SetFor(1, false, time.Minute)
	value, ok := c.Get(1)
	require.True(t, ok, "cached zero value must be distinguishable from a miss")
	require.False(t, value)

	c.Invalidate(1)
	_, ok = c.Get(1)
	require.False(t, ok, "invalidated entry must be absent")
}

func TestEntriesExpire(t *testing.T) {
	tests := []struct {
		name string
		set  func(c *cache.Cache[string])
	}{
		{name: "expires entries after the default TTL", set: func(c *cache.Cache[string]) { c.Set("key", "value") }},
		{name: "expires entries after an explicit TTL", set: func(c *cache.Cache[string]) { c.SetFor("key", "value", time.Nanosecond) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := cache.New[string](1000, 20*time.Millisecond)
			defer c.Close()

			tc.set(c)

			require.Eventually(t, func() bool {
				_, ok := c.Get("key")
				return !ok
			}, time.Second, 5*time.Millisecond)
		})
	}
}

func TestInvalidateForcesReload(t *testing.T) {
	c := cache.New[bool](cache.DefaultCapacity, time.Minute)
	defer c.Close()
	var current atomic.Bool
	current.Store(true)
	loader := func(context.Context) (bool, error) { return current.Load(), nil }
	load := func() bool {
		value, err := c.GetOrLoad(context.Background(), "key", loader)
		require.NoError(t, err)
		return value
	}

	require.True(t, load())
	current.Store(false)
	require.True(t, load(), "cached value must survive until invalidation")

	c.Invalidate("key")
	require.False(t, load(), "invalidation must rerun the loader")
}

func TestLenAndCapacity(t *testing.T) {
	c := cache.New[int](128, time.Minute)
	defer c.Close()

	assert.Equal(t, int64(128), c.Capacity())
	assert.Equal(t, 0, c.Len())

	c.Set("a", 1)
	c.Set("b", 2)

	require.Eventually(t, func() bool { return c.Len() == 2 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, int64(128), c.Capacity())
}

func TestKeyedHitSavesStringAlloc(t *testing.T) {
	ctx := context.Background()
	const id uint64 = 123456789

	keyed := cache.NewKeyed[uint64, bool](cache.DefaultCapacity, time.Minute, uintKey)
	defer keyed.Close()
	loader := func(context.Context) (bool, error) { return true, nil }
	_, err := keyed.GetOrLoad(ctx, id, loader)
	require.NoError(t, err)

	stringKeyed := cache.New[bool](cache.DefaultCapacity, time.Minute)
	defer stringKeyed.Close()
	_, err = stringKeyed.GetOrLoad(ctx, uintKey(id), loader)
	require.NoError(t, err)

	uintAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = keyed.GetOrLoad(ctx, id, loader)
	})
	stringAllocs := testing.AllocsPerRun(1000, func() {
		_, _ = stringKeyed.GetOrLoad(ctx, uintKey(id), loader)
	})

	assert.LessOrEqual(t, uintAllocs, 1.0, "uint64 hit allocs/op (theine read-tracking floor)")
	assert.Less(t, uintAllocs, stringAllocs, "uint64 hit must allocate strictly less than the string-keyed hit")
}

func TestCacheKeyFormats(t *testing.T) {
	assert.Equal(t, "user:0", cache.UserKey("user:", 0))
	assert.Equal(t, "modules:123456", cache.UserKey("modules:", 123456))
	assert.Equal(t, "x:18446744073709551615", cache.UserKey("x:", ^uint64(0)))
	assert.Equal(t, "timer:42:daily", cache.PairKey("timer:", 42, "daily"))
}

func TestUserKeyAllocatesOnce(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		_ = cache.UserKey("settings:", 123456789)
	})

	assert.LessOrEqual(t, allocs, 1.0)
}
