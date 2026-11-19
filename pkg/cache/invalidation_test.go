// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/pkg/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidationDoesNotResurrectAnInflightLoad(t *testing.T) {
	mutations := map[string]func(*cache.Cache[int]){
		"key":       func(c *cache.Cache[int]) { c.Invalidate("key") },
		"predicate": func(c *cache.Cache[int]) { c.InvalidateWhere(func(k string) bool { return k == "key" }) },
		"clear":     func(c *cache.Cache[int]) { c.Clear() },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			c := cache.New[int](16, time.Minute)
			defer c.Close()
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan int, 1)
			go func() {
				value, err := c.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) {
					close(entered)
					<-release
					return 1, nil
				})
				assert.NoError(t, err)
				finished <- value
			}()
			<-entered
			mutate(c)
			close(release)
			assert.Equal(t, 1, <-finished)
			_, found := c.Get("key")
			assert.False(t, found)

			value, err := c.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) { return 2, nil })
			require.NoError(t, err)
			assert.Equal(t, 2, value)
		})
	}
}

func TestInvalidatingAnotherKeyKeepsAnInflightLoad(t *testing.T) {
	for _, predicate := range []bool{false, true} {
		t.Run(strconv.FormatBool(predicate), func(t *testing.T) { assertUnrelatedLoadRetained(t, predicate) })
	}
}

func assertUnrelatedLoadRetained(t *testing.T, predicate bool) {
	t.Helper()
	stored := false
	for attempt := 0; attempt < 3 && !stored; attempt++ {
		c := cache.New[int](16, time.Minute)
		c.Set("other", 2)
		_, err := c.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) {
			if predicate {
				c.InvalidateWhere(func(key string) bool { return key == "other" })
			} else {
				c.Invalidate("other")
			}
			return 1, nil
		})
		require.NoError(t, err)
		_, stored = c.Get("key")
		c.Close()
	}
	assert.True(t, stored, "a write to one key must not discard loads of other keys")
}

type invalidationFeed struct {
	watches map[string]func(cache.Event)
	gap     func()
}

func (f *invalidationFeed) Watch(scope string, fn func(cache.Event)) { f.watches[scope] = fn }
func (f *invalidationFeed) WatchGap(fn func())                       { f.gap = fn }

func TestCacheBindingsEvictOnlyTheirKeysAndClearOnGap(t *testing.T) {
	f := &invalidationFeed{watches: make(map[string]func(cache.Event))}
	c := cache.New[int](16, time.Minute).InvalidateOn(f, cache.UserKeys("user:"), "status", "grant")
	defer c.Close()
	for _, scope := range []string{"status", "grant"} {
		c.Set("user:7", 7)
		c.Set("user:8", 8)
		f.watches[scope](cache.Event{ID: 7})
		_, invalidated := c.Get("user:7")
		_, kept := c.Get("user:8")
		assert.Equal(t, [2]bool{false, true}, [2]bool{invalidated, kept})
	}
	for _, key := range []string{"user:8", "user:9"} {
		c.Set(key, 1)
	}
	f.gap()
	for _, key := range []string{"user:8", "user:9"} {
		_, found := c.Get(key)
		assert.False(t, found)
	}
}

func TestClearAndUnboundCache(t *testing.T) {
	c := cache.New[int](16, time.Minute).InvalidateOn(nil, cache.UserKeys(""), "status")
	defer c.Close()
	for _, key := range []string{"first", "second", "third"} {
		c.Set(key, 1)
	}
	c.Clear()
	for _, key := range []string{"first", "second", "third"} {
		_, found := c.Get(key)
		assert.False(t, found)
	}
}
