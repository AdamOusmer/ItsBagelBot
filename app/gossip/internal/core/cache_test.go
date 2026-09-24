// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

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

type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return b, ok, nil
}

func (s *memStore) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	return nil
}

func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memStore) SetNX(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = []byte("1")
	return true, nil
}

type payload struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func TestCachedMissFillsThenHits(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	fetch := func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "x", N: 7}, nil
	}

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)

	got, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)
	assert.Equal(t, int32(1), fetches.Load(), "second read must come from cache")
}

func TestCachedAdmitSkippedOnHit(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	fill := func(context.Context) (payload, error) { return payload{Name: "x", N: 1}, nil }

	_, err := Cached(ctx, c, "k", time.Minute, time.Minute, nil, fill)
	require.NoError(t, err)

	got, err := Cached(ctx, c, "k", time.Minute, time.Minute, func(context.Context) error {
		t.Error("a hit must not spend budget")
		return nil
	}, fill)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 1}, got)
}

func TestCachedAdmitIsPerCallerUnderOneFlight(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	denied := &UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

	const perLane = 4
	var fetches atomic.Int32
	release := make(chan struct{})
	fill := func(context.Context) (payload, error) {
		<-release
		fetches.Add(1)
		return payload{Name: "x", N: 1}, nil
	}

	premium, standard := make([]error, perLane), make([]error, perLane)
	var admitted, wg sync.WaitGroup
	admitted.Add(2 * perLane)
	fire := func(out []error, verdict error) {
		for i := range out {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				admit := func(context.Context) error {
					admitted.Done()
					return verdict
				}
				_, out[i] = Cached(ctx, c, "k", time.Minute, time.Minute, admit, fill)
			}(i)
		}
	}
	fire(premium, nil)
	fire(standard, denied)

	admitted.Wait()
	close(release)
	wg.Wait()

	for i, err := range premium {
		assert.NoError(t, err, "premium caller %d must not inherit the standard lane's denial", i)
	}
	for i, err := range standard {
		assert.ErrorIs(t, err, denied, "standard caller %d must be denied by its own lane", i)
	}
	assert.Equal(t, int32(1), fetches.Load(), "the flight must still cost one upstream call")
}

func TestCachedErrorNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	boom := errors.New("boom")

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, boom
	})
	require.ErrorIs(t, err, boom)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "ok"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got.Name)
	assert.Equal(t, int32(2), fetches.Load(), "a failed fetch must be retried, never cached")
}

func TestCachedNegativeCache(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	notFound := &UpstreamError{Status: 404, Message: "player not found"}

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, notFound
	})
	assert.Equal(t, notFound, err)

	_, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, notFound
	})
	assert.Equal(t, notFound, err)
	assert.Equal(t, int32(1), fetches.Load(), "a 404 fetch must be negatively cached")
}

func TestCachedPoisonEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte("{not json"), time.Minute))
	c := NewCache(st)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		return payload{Name: "fresh"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.Name)
}

func TestCachedLegacyFormatEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte(`{"name":"old-format","n":42}`), time.Minute))
	c := NewCache(st)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		return payload{Name: "fresh", N: 7}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "fresh", N: 7}, got)

	got, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		t.Error("must not refetch a repaired entry")
		return payload{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.Name)
}

func TestCachedZeroValueSuccessRoundTrips(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32

	for range 2 {
		v, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (string, error) {
			fetches.Add(1)
			return "", nil
		})
		require.NoError(t, err)
		assert.Empty(t, v)
	}
	assert.Equal(t, int32(1), fetches.Load(), "empty-string success must be served from cache")
}

func TestCachedRateLimitNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	busy := &UpstreamError{Status: 429, Message: "busy"}

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, busy
	})
	assert.Equal(t, busy, err)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "recovered"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "recovered", got.Name)
	assert.Equal(t, int32(2), fetches.Load(), "a 429 must be retried, never cached")
}

func TestCachedNegativeSharedAcrossInstances(t *testing.T) {
	st := newMemStore()
	notFound := &UpstreamError{Status: 404, Message: "player not found"}

	_, err := Cached(context.Background(), NewCache(st), "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		return payload{}, notFound
	})
	assert.Equal(t, notFound, err)

	_, err = Cached(context.Background(), NewCache(st), "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		t.Error("second replica must serve the negative from the shared store")
		return payload{}, nil
	})
	var ue *UpstreamError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, 404, ue.Status)
	assert.Equal(t, "player not found", ue.Message)
}

func TestCachedSingleflightCollapses(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	release := make(chan struct{})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
				fetches.Add(1)
				<-release
				return payload{Name: "one"}, nil
			})
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	assert.Equal(t, int32(1), fetches.Load(), "concurrent misses must share one fetch")
}

func TestSnapshotRoundTrip(t *testing.T) {
	c := NewCache(newMemStore())
	require.NoError(t, c.SetJSON(context.Background(), "snap", payload{Name: "s", N: 3}, time.Hour))

	var got payload
	ok, err := c.GetJSON(context.Background(), "snap", &got)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, payload{Name: "s", N: 3}, got)

	ok, err = c.GetJSON(context.Background(), "missing", &got)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestKey(t *testing.T) {
	assert.Equal(t, "gossip:urchin:daily:techno", Key("urchin", "daily", "techno"))
}

func TestCachedStaleServedThenRevalidated(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	var fetches atomic.Int32

	fill := func(n int) func(context.Context) (payload, error) {
		return func(context.Context) (payload, error) {
			fetches.Add(1)
			return payload{Name: "x", N: n}, nil
		}
	}

	got, err := Cached(ctx, c, "k", 20*time.Millisecond, time.Minute, nil, fill(1))
	require.NoError(t, err)
	require.Equal(t, 1, got.N)
	time.Sleep(40 * time.Millisecond)

	got, err = Cached(ctx, c, "k", time.Minute, time.Minute, nil, fill(2))
	require.NoError(t, err)
	assert.Equal(t, 1, got.N, "a stale read must serve the stored value, not block on the refetch")

	require.Eventually(t, func() bool {
		v, gerr := Cached(ctx, c, "k", time.Minute, time.Minute, nil, fill(2))
		return gerr == nil && v.N == 2
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), fetches.Load(), "one cold fill plus exactly one revalidation")
}

func TestCachedStaleRefreshClaimedOnceFleetWide(t *testing.T) {
	st := newMemStore()
	podA, podB := NewCache(st), NewCache(st)
	ctx := context.Background()
	var fetches atomic.Int32

	_, err := Cached(ctx, podA, "k", 20*time.Millisecond, time.Minute, nil,
		func(context.Context) (payload, error) {
			fetches.Add(1)
			return payload{Name: "x", N: 1}, nil
		})
	require.NoError(t, err)
	time.Sleep(40 * time.Millisecond)

	release := make(chan struct{})
	refetch := func(context.Context) (payload, error) {
		<-release
		fetches.Add(1)
		return payload{Name: "x", N: 2}, nil
	}
	for _, pod := range []*Cache{podA, podB} {
		got, gerr := Cached(ctx, pod, "k", time.Minute, time.Minute, nil, refetch)
		require.NoError(t, gerr)
		assert.Equal(t, 1, got.N, "stale read must serve the old value")
	}
	close(release)

	require.Eventually(t, func() bool {
		v, gerr := Cached(ctx, podA, "k", time.Minute, time.Minute, nil, refetch)
		return gerr == nil && v.N == 2
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), fetches.Load(), "one cold fill plus exactly one fleet-wide refresh")
}

func TestCachedNegativeIsNotRevalidated(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	missing := &UpstreamError{Status: 404, Message: "player not found"}
	var fetches atomic.Int32

	fetch := func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, missing
	}

	for range 3 {
		_, err := Cached(ctx, c, "k", time.Minute, time.Minute, nil, fetch)
		var ue *UpstreamError
		require.ErrorAs(t, err, &ue)
		assert.Equal(t, 404, ue.Status)
		assert.Equal(t, "player not found", ue.Message)
	}
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), fetches.Load(), "a cached negative must not be refetched")
}

func TestCachedLegacyEntryWithoutStampRefreshes(t *testing.T) {
	st := newMemStore()
	ctx := context.Background()
	require.NoError(t, st.Set(ctx, "k", []byte(`{"v":{"name":"old","n":1}}`), time.Minute))
	c := NewCache(st)
	var fetches atomic.Int32

	got, err := Cached(ctx, c, "k", time.Minute, time.Minute, nil,
		func(context.Context) (payload, error) {
			fetches.Add(1)
			return payload{Name: "new", N: 2}, nil
		})
	require.NoError(t, err)
	assert.Equal(t, "old", got.Name, "the legacy value is still served, not discarded")

	require.Eventually(t, func() bool {
		v, gerr := Cached(ctx, c, "k", time.Minute, time.Minute, nil,
			func(context.Context) (payload, error) { return payload{Name: "new", N: 2}, nil })
		return gerr == nil && v.Name == "new"
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(1), fetches.Load())
}

func TestStoreCachedValueIsServedAsHit(t *testing.T) {
	c := NewCache(newMemStore())
	StoreCached(context.Background(), c, StoreRequest[payload]{
		Key: "k", TTL: time.Minute, NegativeTTL: time.Minute,
		Value: payload{Name: "hydrated", N: 7},
	})

	v, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		t.Error("a hydrated entry must be served without a fetch")
		return payload{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "hydrated", N: 7}, v)
}

func TestStoreCachedNegativeIsServedAsHit(t *testing.T) {
	c := NewCache(newMemStore())
	notFound := &UpstreamError{Status: 404, Message: "player not found"}
	StoreCached(context.Background(), c, StoreRequest[payload]{
		Key: "k", TTL: time.Minute, NegativeTTL: time.Minute,
		Err: notFound,
	})

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		t.Error("a hydrated negative must be served without a fetch")
		return payload{}, nil
	})
	assert.Equal(t, notFound, err)
}

func TestStoreCachedInfraFailureStoresNothing(t *testing.T) {
	c := NewCache(newMemStore())
	StoreCached(context.Background(), c, StoreRequest[payload]{
		Key: "k", TTL: time.Minute, NegativeTTL: time.Minute,
		Err: errors.New("upstream exploded"),
	})

	var fetches atomic.Int32
	v, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, nil, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "fetched"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "fetched"}, v)
	assert.Equal(t, int32(1), fetches.Load(), "an infra failure must leave the key uncached")
}

func TestCacheID(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{parts: []string{"  FrOsTy  ", "6"}, want: "frosty:6"},
		{parts: []string{"0", " Ca ", "predicted"}, want: "0:ca:predicted"},
		{parts: []string{"", "kr", "pc"}, want: ":kr:pc"},
		{parts: []string{"solo"}, want: "solo"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, CacheID(c.parts...), "parts %q", c.parts)
	}
}
