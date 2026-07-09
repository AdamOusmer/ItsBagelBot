package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildStatic(body string, ttl time.Duration, counter *atomic.Int32) func(context.Context) ([]byte, time.Duration, error) {
	return func(context.Context) ([]byte, time.Duration, error) {
		if counter != nil {
			counter.Add(1)
		}
		return []byte(body), ttl, nil
	}
}

func TestCachedBytesMissFillsThenHits(t *testing.T) {
	c := NewCache(newMemStore())
	var builds atomic.Int32

	b, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"player":"x"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b))

	b, err = CachedBytes(context.Background(), c, "k", buildStatic(`{"player":"other"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b), "hit must serve the stored bytes")
	assert.Equal(t, int32(1), builds.Load())
}

// TTL zero answers without storing: the next request rebuilds. This is how a
// rate-limit denial stays friendly but never pins.
func TestCachedBytesZeroTTLNotStored(t *testing.T) {
	c := NewCache(newMemStore())
	var builds atomic.Int32

	_, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"error":"busy"}`, 0, &builds))
	require.NoError(t, err)

	b, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"player":"x"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b))
	assert.Equal(t, int32(2), builds.Load(), "a ttl-zero reply must not be cached")
}

func TestCachedBytesBuildErrorPropagates(t *testing.T) {
	c := NewCache(newMemStore())
	boom := errors.New("boom")

	_, err := CachedBytes(context.Background(), c, "k", func(context.Context) ([]byte, time.Duration, error) {
		return nil, 0, boom
	})
	require.ErrorIs(t, err, boom)

	// Nothing cached: the next build runs and succeeds.
	b, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"ok":true}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(b))
}

// A legacy/foreign entry (no marker prefix) must read as poison and refetch —
// never be served as a reply.
func TestCachedBytesLegacyEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte(`{"player":"old-format"}`), time.Minute))
	c := NewCache(st)

	b, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"player":"fresh"}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))

	// The repaired entry serves without another build.
	b, err = CachedBytes(context.Background(), c, "k", func(context.Context) ([]byte, time.Duration, error) {
		t.Error("must not rebuild a repaired entry")
		return nil, 0, nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))
}

// Entries survive across Cache instances (replicas sharing the valkey store).
func TestCachedBytesSharedAcrossInstances(t *testing.T) {
	st := newMemStore()
	_, err := CachedBytes(context.Background(), NewCache(st), "k", buildStatic(`{"player":"x"}`, time.Minute, nil))
	require.NoError(t, err)

	b, err := CachedBytes(context.Background(), NewCache(st), "k", func(context.Context) ([]byte, time.Duration, error) {
		t.Error("second replica must serve from the shared store")
		return nil, 0, nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b))
}

func TestUnwrapEntry(t *testing.T) {
	fresh, payload, ok := unwrapEntry([]byte(`{"gw2":123,"p":{"a":1}}`))
	require.True(t, ok)
	assert.Equal(t, int64(123), fresh)
	assert.Equal(t, `{"a":1}`, string(payload))

	// Rejected: empty, non-entry JSON, truncations, a missing/empty fresh stamp,
	// an empty payload, and the legacy {"gw1":…} marker (format bump = poison).
	for _, bad := range []string{
		"", "{}", `{"gw2":`, `{"gw2":123}`, `{"gw2":,"p":{}}`, `{"gw2":123,"p":}`,
		`{"player":"x"}`, `{"gw1":{"a":1}}`,
	} {
		_, _, ok := unwrapEntry([]byte(bad))
		assert.False(t, ok, "must reject %q", bad)
	}
}

// A stale entry is served immediately and revalidated in the background, so the
// slow upstream stays off the caller's path after the first cold fill.
func TestCachedBytesStaleServedThenRevalidated(t *testing.T) {
	c := NewCache(newMemStore())
	var builds atomic.Int32
	ctx := context.Background()

	// Cold fill, fresh for 20ms.
	b, err := CachedBytes(ctx, c, "k", buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b))
	require.Equal(t, int32(1), builds.Load())

	// Let the fresh window lapse. memStore keeps the bytes; SWR is driven by the
	// embedded fresh-until stamp, not physical expiry.
	time.Sleep(40 * time.Millisecond)

	// Stale hit: returns the OLD bytes at once and kicks one background rebuild.
	b, err = CachedBytes(ctx, c, "k", buildStatic(`{"n":2}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b), "stale hit must serve the old bytes")

	// The revalidation lands the new value; once fresh, later reads serve it.
	require.Eventually(t, func() bool {
		got, gerr := CachedBytes(ctx, c, "k", buildStatic(`{"n":2}`, time.Minute, &builds))
		return gerr == nil && string(got) == `{"n":2}`
	}, time.Second, 10*time.Millisecond)
}

// The reply-bytes hit path is the gateway's hot path: after the store read it
// must do no JSON work and no allocation (prefix check + slice only).
func BenchmarkCachedBytesHit(b *testing.B) {
	st := newMemStore()
	c := NewCache(st)
	_, err := CachedBytes(context.Background(), c, "k", buildStatic(`{"player":"Techno","wins":5,"losses":2}`, time.Hour, nil))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := CachedBytes(ctx, c, "k", nil); err != nil {
			b.Fatal(err)
		}
	}
}
