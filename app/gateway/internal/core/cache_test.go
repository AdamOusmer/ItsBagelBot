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

	got, err := Cached(context.Background(), c, "k", time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)

	got, err = Cached(context.Background(), c, "k", time.Minute, fetch)
	require.NoError(t, err)
	assert.Equal(t, payload{Name: "x", N: 7}, got)
	assert.Equal(t, int32(1), fetches.Load(), "second read must come from cache")
}

func TestCachedErrorNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	var fetches atomic.Int32
	boom := errors.New("boom")

	_, err := Cached(context.Background(), c, "k", time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{}, boom
	})
	require.ErrorIs(t, err, boom)

	got, err := Cached(context.Background(), c, "k", time.Minute, func(context.Context) (payload, error) {
		fetches.Add(1)
		return payload{Name: "ok"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got.Name)
	assert.Equal(t, int32(2), fetches.Load(), "a failed fetch must be retried, never cached")
}

func TestCachedPoisonEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte("{not json"), time.Minute))
	c := NewCache(st)

	got, err := Cached(context.Background(), c, "k", time.Minute, func(context.Context) (payload, error) {
		return payload{Name: "fresh"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.Name)
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
			_, _ = Cached(context.Background(), c, "k", time.Minute, func(context.Context) (payload, error) {
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
