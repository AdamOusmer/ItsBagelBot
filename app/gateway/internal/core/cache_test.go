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

// memStore is an in-memory Store for tests (TTL ignored beyond storage).
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
	s.m[key] = val
	return nil
}

func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
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

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)

	got, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)
	assert.Equal(t, int32(1), fetches.Load(), "second read must come from cache")
}

func TestCachedErrorNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	boom := errors.New("boom")

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, boom
	})
	require.ErrorIs(t, err, boom)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
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

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, notFound
	})
	assert.Equal(t, notFound, err)

	_, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
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

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		return payload{Name: "fresh"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.Name)
}

// A legacy/foreign-format entry is VALID JSON but carries no envelope marker.
// It once unmarshaled "successfully" into a zero-value envelope and the caller
// served an empty reply (blank player, zero stats) until the entry expired —
// the live "command answers garbage until retried later" bug. It must read as
// poison and refetch instead.
func TestCachedLegacyFormatEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte(`{"name":"old-format","n":42}`), time.Minute))
	c := NewCache(st)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		return payload{Name: "fresh", N: 7}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "fresh", N: 7}, got)

	// And the refreshed entry now serves without another fetch.
	got, err = Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		t.Error("must not refetch a repaired entry")
		return payload{}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.Name)
}

// A zero-value success (empty string) still round-trips: the always-present "v"
// member is the format marker, so it must not be mistaken for a legacy entry.
func TestCachedZeroValueSuccessRoundTrips(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32

	for range 2 {
		v, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (string, error) {
			fetches.Add(1)
			return "", nil
		})
		require.NoError(t, err)
		assert.Empty(t, v)
	}
	assert.Equal(t, int32(1), fetches.Load(), "empty-string success must be served from cache")
}

// A 429 (rate limited) must NOT be negatively cached: the next request should
// retry the bucket, not be pinned to a denial for the negative TTL.
func TestCachedRateLimitNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	busy := &UpstreamError{Status: 429, Message: "busy"}

	_, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, busy
	})
	assert.Equal(t, busy, err)

	got, err := Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "recovered"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "recovered", got.Name)
	assert.Equal(t, int32(2), fetches.Load(), "a 429 must be retried, never cached")
}

// The negative entry must also be honored on the fast path AFTER a fresh Cache
// (fresh singleflight group) reads it — i.e. it survives in the store, not just
// in-process.
func TestCachedNegativeSharedAcrossInstances(t *testing.T) {
	st := newMemStore()
	notFound := &UpstreamError{Status: 404, Message: "player not found"}

	_, err := Cached(context.Background(), NewCache(st), "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
		return payload{}, notFound
	})
	assert.Equal(t, notFound, err)

	// A different Cache instance (another replica) sharing the same store.
	_, err = Cached(context.Background(), NewCache(st), "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
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
			_, _ = Cached(context.Background(), c, "k", time.Minute, time.Minute, func(context.Context) (payload, error) {
				fetches.Add(1)
				<-release
				return payload{Name: "one"}, nil
			})
		}()
	}
	// Give the goroutines a moment to pile onto the flight, then release.
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
	assert.Equal(t, "gateway:urchin:daily:techno", Key("urchin", "daily", "techno"))
}
