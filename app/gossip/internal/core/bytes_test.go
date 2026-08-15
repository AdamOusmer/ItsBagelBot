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

// TTL zero answers without storing: the next request rebuilds. This is how a
// rate-limit denial stays friendly but never pins.
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

	// Nothing cached: the next build runs and succeeds.
	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"ok":true}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(b))
}

// A legacy/foreign entry (no marker prefix) must read as poison and refetch —
// never be served as a reply.
func TestCachedBytesLegacyEntryRefetched(t *testing.T) {
	st := newMemStore()
	require.NoError(t, st.Set(context.Background(), "k", []byte(`{"player":"old-format"}`), time.Minute))
	c := NewCache(st)

	b, err := CachedBytes(context.Background(), c, "k", nil, buildStatic(`{"player":"fresh"}`, time.Minute, nil))
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))

	// The repaired entry serves without another build.
	b, err = CachedBytes(context.Background(), c, "k", nil, func(context.Context) ([]byte, time.Duration, error) {
		t.Error("must not rebuild a repaired entry")
		return nil, 0, nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"player":"fresh"}`, string(b))
}

// Entries survive across Cache instances (replicas sharing the valkey store).
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
	b, err := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b))
	require.Equal(t, int32(1), builds.Load())

	// Let the fresh window lapse. memStore keeps the bytes; SWR is driven by the
	// embedded fresh-until stamp, not physical expiry.
	time.Sleep(40 * time.Millisecond)

	// Stale hit: returns the OLD bytes at once and kicks one background rebuild.
	b, err = CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":2}`, time.Minute, &builds))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, string(b), "stale hit must serve the old bytes")

	// The revalidation lands the new value; once fresh, later reads serve it.
	require.Eventually(t, func() bool {
		got, gerr := CachedBytes(ctx, c, "k", nil, buildStatic(`{"n":2}`, time.Minute, &builds))
		return gerr == nil && string(got) == `{"n":2}`
	}, time.Second, 10*time.Millisecond)
}

// A stale key read on several replicas at once must cost ONE upstream call,
// not one per replica: the SWR refresh is gated on a SET NX claim in the
// SHARED store, so the pod-local dedup maps never decide alone. Two Cache
// instances over one store model two pods; without the claim each would fire
// its own background refresh.
func TestCachedBytesStaleRefreshClaimedOnceFleetWide(t *testing.T) {
	st := newMemStore()
	podA, podB := NewCache(st), NewCache(st)
	var builds atomic.Int32
	ctx := context.Background()

	_, err := CachedBytes(ctx, podA, "k", nil, buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	require.Equal(t, int32(1), builds.Load())
	time.Sleep(40 * time.Millisecond)

	// Both pods take a stale hit and each spawns its refresh goroutine; the
	// claim lets exactly one through.
	//
	// The rebuild parks until both stale reads are done. An instant rebuild lets
	// the winning pod's refresh store the fresh value BETWEEN the two reads, and
	// the second pod then takes an ordinary fresh hit and serves {"n":2} — a
	// perfectly correct outcome, but not the stale path this test exists to
	// pin down, so the assertion below would fail on a timing accident rather
	// than on a real regression.
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

// A cache hit must cost NO budget. The buckets meter upstream spend, so charging
// a hit would meter chat volume instead and drain a daily allowance on requests
// that never left the pod.
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

// A denial belongs to the caller that earned it and must not be cached: the
// bucket refills in seconds, so the very next request has to be able to retry.
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

// THE premium-reserve regression test. Concurrent callers for one key collapse
// into a single flight, and the budget check used to live inside that flight —
// so whichever caller won it ran the check for everyone, and a standard caller
// with a drained bucket handed its 429 to premium callers who were entitled to
// the reserve. Admission is per caller now: every denied caller gets its own
// denial, every allowed caller gets the bytes, and the flight still costs ONE
// upstream call.
func TestCachedBytesAdmitIsPerCallerUnderOneFlight(t *testing.T) {
	c := NewCache(newMemStore())
	ctx := context.Background()
	denied := &UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

	const callers = 8
	var builds atomic.Int32
	release := make(chan struct{})
	// The build parks until every caller has been admitted, forcing them all into
	// the same flight; otherwise an early finisher fills the key and the later
	// callers take fresh hits and never exercise admission at all.
	build := func(bctx context.Context) ([]byte, time.Duration, error) {
		<-release
		return buildStatic(`{"n":1}`, time.Minute, &builds)(bctx)
	}

	type outcome struct {
		body []byte
		err  error
	}
	results := make([]outcome, callers)
	var admitted, wg sync.WaitGroup
	admitted.Add(callers)
	wg.Add(callers)
	for i := range callers {
		go func(i int) {
			defer wg.Done()
			// Odd callers are the drained standard lane, even ones are premium.
			admit := func(context.Context) error {
				admitted.Done()
				if i%2 == 1 {
					return denied
				}
				return nil
			}
			body, err := CachedBytes(ctx, c, "k", admit, build)
			results[i] = outcome{body: body, err: err}
		}(i)
	}
	admitted.Wait()
	close(release)
	wg.Wait()

	for i, got := range results {
		if i%2 == 1 {
			assert.ErrorIs(t, got.err, denied, "caller %d was denied by its own lane", i)
			continue
		}
		require.NoError(t, got.err, "caller %d must not inherit another lane's denial", i)
		assert.JSONEq(t, `{"n":1}`, string(got.body))
	}
	assert.Equal(t, int32(1), builds.Load(), "the flight must still cost one upstream call")
}

// The reply-bytes hit path is gossip's hot path: after the store read it
// must do no JSON work and no allocation (prefix check + slice only).
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
