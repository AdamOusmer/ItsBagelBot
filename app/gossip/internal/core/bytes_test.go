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

	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"x"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b))

	b, err = CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"other"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b), "hit must serve the stored bytes")
	assert.Equal(t, int32(1), builds.Load())
}

func TestCachedBytesZeroTTLNotStored(t *testing.T) {
	c := NewCache(newMemStore())
	var builds atomic.Int32

	_, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"error":"busy"}`, 0, &builds))
	require.NoError(t, err)

	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"x"}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"x"}`, string(b))
	assert.Equal(t, int32(2), builds.Load(), "a ttl-zero reply must not be cached")
}

func TestCachedBytesBuildErrorPropagates(t *testing.T) {
	c := NewCache(newMemStore())
	boom := errors.New("boom")

	_, err := CachedBytes(context.Background(), c, "k", nil, func(context.Context) ([]byte, time.Duration, error) {
		return nil, 0, boom
	})
	require.ErrorIs(t, err, boom)

	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"ok":true}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(b))
}

func TestCachedBytesLegacyEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte(`{"player":"old-format"}`), time.Minute))
	c := NewCache(st)

	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"fresh"}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))

	b, err = CachedBytes(context.Background(), c, "k", nil, func(context.Context) ([]byte, time.Duration, error) {
		t.Error("must not rebuild a repaired entry")
		return nil, 0, nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))
}

func TestCachedBytesSharedAcrossInstances(t *testing.T) {
	st := newMemStore()
	_, err := CachedBytes(context.Background(), NewCache(st), "k", nil, buildStatic(`{"player":"x"}`, time.Minute, nil))
	require.NoError(t, err)

	b, err := CachedBytes(context.Background(), NewCache(st), "k", nil, func(context.Context) ([]byte, time.Duration, error) {
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

	for _, bad := range []string{
		"", "{}", `{"gw2":`, `{"gw2":123}`, `{"gw2":,"p":{}}`, `{"gw2":123,"p":}`,
		`{"player":"x"}`, `{"gw1":{"a":1}}`,
	} {
		_, _, ok := unwrapEntry([]byte(bad))
		assert.False(t, ok, "must reject %q", bad)
	}
}

func TestCachedBytesStaleServedThenRevalidated(t *testing.T) {
	c := NewCache(newMemStore())
	var builds atomic.Int32
	ctx := context.Background()

	b, err := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b))
	require.Equal(t, int32(1), builds.Load())

	time.Sleep(40 * time.Millisecond)

	b, err = CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":2}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b), "stale hit must serve the old bytes")

	require.Eventually(t, func() bool {
		got, gerr := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":2}`, time.Minute, &builds))
		return gerr == nil && string(got) == `{"n":2}`
	}, time.Second, 10*time.Millisecond)
}

func TestCachedBytesStaleRefreshClaimedOnceFleetWide(t *testing.T) {
	st := newMemStore()
	podA, podB := NewCache(st), NewCache(st)
	var builds atomic.Int32
	ctx := context.Background()

	_, err := CachedBytes(ctx, podA, "k", nil, buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	require.Equal(t, int32(1), builds.Load())
	time.Sleep(40 * time.Millisecond)

	release := make(chan struct{})
	rebuild := func(rctx context.Context) ([]byte, time.Duration, error) {
		<-release
		return buildStatic(`{"n":2}`, time.Minute, &builds)(rctx)
	}
	for _, pod := range []*Cache{podA, podB} {
		b, gerr := CachedBytes(ctx, pod, "k", nil, rebuild)
		require.NoError(t, gerr)
		assert.JSONEq(t, `{"n":1}`, string(b), "stale hit must serve the old bytes")
	}
	close(release)

	require.Eventually(t, func() bool {
		got, gerr := CachedBytes(ctx, podA, "k", nil, rebuild)
		return gerr == nil && string(got) == `{"n":2}`
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), builds.Load(), "one cold fill + exactly one fleet-wide refresh")
}

func TestCachedBytesAdmitSkippedOnFreshHit(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()

	_, err := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":1}`, time.Minute, nil))
	require.NoError(t, err)

	b, err := CachedBytes(ctx, c, "k", func(context.Context) error {
		t.Error("a fresh hit must not spend budget")
		return nil
	}, buildStatic(`{"n":2}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b))
}

func TestCachedBytesAdmitDenialIsNotCached(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	denied := &UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

	_, err := CachedBytes(ctx, c, "k", func(context.Context) error { return denied },
		func(context.Context) ([]byte, time.Duration, error) {
			t.Error("a denied caller must never reach the upstream")
			return nil, 0, nil
		})
	require.ErrorIs(t, err, denied)

	b, err := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":1}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b), "a denial must not poison the key")
}

func TestCachedBytesAdmitIsPerCallerUnderOneFlight(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	denied := &UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

	const perLane = 4
	var builds atomic.Int32
	release := make(chan struct{})
	build := func(bctx context.Context) ([]byte, time.Duration, error) {
		<-release
		return buildStatic(`{"n":1}`, time.Minute, &builds)(bctx)
	}

	premium, standard := make([]laneOutcome, perLane), make([]laneOutcome, perLane)
	var admitted, wg sync.WaitGroup
	admitted.Add(2 * perLane)
	fire := func(out []laneOutcome, verdict error) {
		for i := range out {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				admit := func(context.Context) error {
					admitted.Done()
					return verdict
				}
				body, err := CachedBytes(ctx, c, "k", admit, build)
				out[i] = laneOutcome{body: body, err: err}
			}(i)
		}
	}
	fire(premium, nil)
	fire(standard, denied)

	admitted.Wait()
	close(release)
	wg.Wait()

	for i, got := range premium {
		require.NoError(t, got.err, "premium caller %d must not inherit the standard lane's denial", i)
		assert.JSONEq(t, `{"n":1}`, string(got.body))
	}
	for i, got := range standard {
		assert.ErrorIs(t, got.err, denied, "standard caller %d must be denied by its own lane", i)
	}
	assert.Equal(t, int32(1), builds.Load(), "the flight must still cost one upstream call")
}

type laneOutcome struct {
	body []byte
	err  error
}

func BenchmarkCachedBytesHit(b *testing.B) {
	st := newMemStore()
	c := NewCache(st)
	_, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"Techno","wins":5,"losses":2}`, time.Hour, nil))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := CachedBytes(ctx, c, "k", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}
