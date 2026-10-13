// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

const (
	staleTestKey  = "key"
	staleShortTTL = 10 * time.Millisecond
	staleExpired  = 30 * time.Millisecond
)

var errReload = errors.New("reload failed")

func failingReload(context.Context) (string, error) { return "", errReload }

func succeedingReload(context.Context) (string, error) { return "new", nil }

func TestKeyedStaleOnError(t *testing.T) {
	cases := []struct {
		name       string
		ttl        time.Duration
		window     time.Duration
		wait       time.Duration
		before     func(*Cache[string])
		during     func(*Cache[string])
		reload     func(context.Context) (string, error)
		want       string
		wantErr    error
		wantCalls  int32
		wantFresh  bool
		wantStored bool
	}{
		{
			name: "fresh hit skips loader", ttl: time.Minute, window: time.Minute,
			reload: failingReload, want: "old", wantCalls: 0, wantFresh: true, wantStored: true,
		},
		{
			name: "soft expired with failing loader serves stale", ttl: staleShortTTL, window: time.Minute, wait: staleExpired,
			reload: failingReload, want: "old", wantCalls: 1, wantFresh: false, wantStored: true,
		},
		{
			name: "soft expired with succeeding loader stores fresh value", ttl: staleShortTTL, window: time.Minute, wait: staleExpired,
			reload: succeedingReload, want: "new", wantCalls: 1, wantFresh: true, wantStored: true,
		},
		{
			name: "past window with failing loader errors", ttl: staleShortTTL, window: staleShortTTL, wait: 4 * staleShortTTL,
			reload: failingReload, wantErr: errReload, wantCalls: 1,
		},
		{
			name: "invalidated entry is never served stale", ttl: staleShortTTL, window: time.Minute, wait: staleExpired,
			before: func(c *Cache[string]) { c.Invalidate(staleTestKey) },
			reload: failingReload, wantErr: errReload, wantCalls: 1,
		},
		{
			name: "invalidation during reload drops stale value", ttl: staleShortTTL, window: time.Minute, wait: staleExpired,
			during: func(c *Cache[string]) { c.Invalidate(staleTestKey) },
			reload: failingReload, wantErr: errReload, wantCalls: 1,
		},
		{
			name: "without window loader error propagates", ttl: staleShortTTL, wait: staleExpired,
			during: func(c *Cache[string]) { c.Set(staleTestKey, "concurrent") },
			reload: failingReload, wantErr: errReload, wantCalls: 1, wantFresh: true, wantStored: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New[string](1000, tc.ttl, StaleOnError(tc.window))
			defer c.Close()

			c.Set(staleTestKey, "old")
			time.Sleep(tc.wait)
			if tc.before != nil {
				tc.before(c)
			}

			var calls atomic.Int32
			got, err := c.GetOrLoad(context.Background(), staleTestKey, func(ctx context.Context) (string, error) {
				calls.Add(1)
				if tc.during != nil {
					tc.during(c)
				}
				return tc.reload(ctx)
			})

			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantCalls, calls.Load())
			_, fresh := c.Get(staleTestKey)
			assert.Equal(t, tc.wantFresh, fresh, "fresh after read")
			_, stored := c.client.Get(staleTestKey)
			assert.Equal(t, tc.wantStored, stored, "stored after read")
		})
	}
}

func TestKeyedStaleOnErrorSharesOneReload(t *testing.T) {
	c := New[string](1000, staleShortTTL, StaleOnError(time.Minute))
	defer c.Close()

	c.Set(staleTestKey, "old")
	time.Sleep(staleExpired)

	var calls atomic.Int32
	release := make(chan struct{})
	loader := func(context.Context) (string, error) {
		calls.Add(1)
		<-release
		return "", errReload
	}

	const readers = 20
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	wg.Add(readers)
	ready.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			ready.Done()
			got, err := c.GetOrLoad(context.Background(), staleTestKey, loader)
			assert.NoError(t, err)
			assert.Equal(t, "old", got)
		}()
	}
	ready.Wait()
	time.Sleep(5 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Less(t, calls.Load(), int32(readers/2), "readers must share the failing reload")
}
