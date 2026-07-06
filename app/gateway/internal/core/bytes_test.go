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
	payload, ok := unwrapEntry([]byte(`{"gw1":{"a":1}}`))
	require.True(t, ok)
	assert.Equal(t, `{"a":1}`, string(payload))

	for _, bad := range []string{"", "{}", `{"gw1":`, `{"player":"x"}`, `{"gw2":{"a":1}}`} {
		_, ok := unwrapEntry([]byte(bad))
		assert.False(t, ok, "must reject %q", bad)
	}
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
