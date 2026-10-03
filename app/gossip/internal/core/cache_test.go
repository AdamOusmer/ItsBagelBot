// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type (
	admitFunc func(context.Context) error
	fetchFunc func(context.Context) (string, error)
)

type cacheFlavor struct {
	name  string
	get   func(c *core.Cache, key string, admit admitFunc, fetch fetchFunc) (string, error)
	entry func(value string, fresh bool) string
}

const farFutureMS = 4102444800000

func stamp(fresh bool) int64 {
	if fresh {
		return farFutureMS
	}
	return 1
}

var (
	typedFlavor = cacheFlavor{
		name: "typed",
		get: func(c *core.Cache, key string, admit admitFunc, fetch fetchFunc) (string, error) {
			return core.Cached(context.Background(), c, key, time.Minute, time.Minute, admit, fetch)
		},
		entry: func(value string, fresh bool) string {
			return fmt.Sprintf(`{"v":%q,"f":%d}`, value, stamp(fresh))
		},
	}
	bytesFlavor = cacheFlavor{
		name: "bytes",
		get: func(c *core.Cache, key string, admit admitFunc, fetch fetchFunc) (string, error) {
			b, err := core.CachedBytes(context.Background(), c, key, admit,
				func(ctx context.Context) ([]byte, time.Duration, error) {
					v, ferr := fetch(ctx)
					return []byte(v), time.Minute, ferr
				})
			return string(b), err
		},
		entry: func(value string, fresh bool) string {
			return fmt.Sprintf(`{"gw2":%d,"p":%s}`, stamp(fresh), value)
		},
	}
	flavors = []cacheFlavor{typedFlavor, bytesFlavor}
)

func eachFlavor(t *testing.T, run func(t *testing.T, f cacheFlavor)) {
	t.Helper()
	for _, f := range flavors {
		t.Run(f.name, func(t *testing.T) { run(t, f) })
	}
}

func fetchOf(calls *atomic.Int32, value string, err error) fetchFunc {
	return func(context.Context) (string, error) {
		calls.Add(1)
		return value, err
	}
}

func seed(t *testing.T, store *providertest.MemStore, raw string) {
	t.Helper()
	require.NoError(t, store.Set(context.Background(), "k", []byte(raw), time.Minute))
}

func TestCacheFillsOnMissThenServesFromTheStore(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		c := core.NewCache(providertest.NewMemStore())
		var fetches atomic.Int32

		got, err := f.get(c, "k", nil, fetchOf(&fetches, "first", nil))
		require.NoError(t, err)
		assert.Equal(t, "first", got)

		got, err = f.get(c, "k", nil, fetchOf(&fetches, "second", nil))
		require.NoError(t, err)
		assert.Equal(t, "first", got)
		assert.EqualValues(t, 1, fetches.Load())
	})
}

func TestCacheDoesNotStoreAFailedFetch(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		c := core.NewCache(providertest.NewMemStore())
		var fetches atomic.Int32
		boom := errors.New("boom")

		_, err := f.get(c, "k", nil, fetchOf(&fetches, "", boom))
		require.ErrorIs(t, err, boom)

		got, err := f.get(c, "k", nil, fetchOf(&fetches, "ok", nil))
		require.NoError(t, err)
		assert.Equal(t, "ok", got)
		assert.EqualValues(t, 2, fetches.Load())
	})
}

func TestCacheEntriesAreSharedAcrossReplicas(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		store := providertest.NewMemStore()
		var fetches atomic.Int32

		_, err := f.get(core.NewCache(store), "k", nil, fetchOf(&fetches, "x", nil))
		require.NoError(t, err)
		got, err := f.get(core.NewCache(store), "k", nil, fetchOf(&fetches, "y", nil))
		require.NoError(t, err)
		assert.Equal(t, "x", got)
		assert.EqualValues(t, 1, fetches.Load())
	})
}

func TestCacheServesStoredEntriesInThePersistedFormat(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		store := providertest.NewMemStore()
		seed(t, store, f.entry(`"stored"`, true))
		var fetches atomic.Int32

		got, err := f.get(core.NewCache(store), "k", nil, fetchOf(&fetches, "fetched", nil))
		require.NoError(t, err)
		assert.Contains(t, got, "stored")
		assert.Zero(t, fetches.Load())
	})
}

func TestCacheRepairsUnreadableEntries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flavor cacheFlavor
		raw    string
	}{
		{"typed: poisoned json", typedFlavor, "{not json"},
		{"typed: pre-envelope format", typedFlavor, `{"name":"old-format","n":42}`},
		{"bytes: empty value", bytesFlavor, ""},
		{"bytes: empty object", bytesFlavor, "{}"},
		{"bytes: truncated marker", bytesFlavor, `{"gw2":`},
		{"bytes: stamp without payload", bytesFlavor, `{"gw2":123}`},
		{"bytes: payload without stamp", bytesFlavor, `{"gw2":,"p":{}}`},
		{"bytes: stamp without value", bytesFlavor, `{"gw2":123,"p":}`},
		{"bytes: pre-marker format", bytesFlavor, `{"player":"old-format"}`},
		{"bytes: retired marker", bytesFlavor, `{"gw1":{"a":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := providertest.NewMemStore()
			seed(t, store, tc.raw)
			c := core.NewCache(store)
			var fetches atomic.Int32

			got, err := tc.flavor.get(c, "k", nil, fetchOf(&fetches, `"fresh"`, nil))
			require.NoError(t, err)
			assert.Contains(t, got, "fresh")

			got, err = tc.flavor.get(c, "k", nil, fetchOf(&fetches, `"again"`, nil))
			require.NoError(t, err)
			assert.Contains(t, got, "fresh", "the repaired entry must be served without another fetch")
			assert.EqualValues(t, 1, fetches.Load())
		})
	}
}

func TestCacheRevalidatesAStaleEntryOnceFleetWide(t *testing.T) {
	eachFlavor(t, func(t *testing.T, f cacheFlavor) {
		store := providertest.NewMemStore()
		seed(t, store, f.entry(`"old"`, false))
		podA, podB := core.NewCache(store), core.NewCache(store)
		var fetches atomic.Int32
		release := make(chan struct{})
		refetch := func(context.Context) (string, error) {
			<-release
			fetches.Add(1)
			return `"new"`, nil
		}

		for _, pod := range []*core.Cache{podA, podB} {
			got, err := f.get(pod, "k", nil, refetch)
			require.NoError(t, err)
			assert.Contains(t, got, "old", "a stale read serves the stored value instead of blocking on the refetch")
		}
		close(release)

		require.Eventually(t, func() bool {
			got, err := f.get(podA, "k", nil, refetch)
			return err == nil && got == `"new"`
		}, time.Second, 10*time.Millisecond)
		assert.EqualValues(t, 1, fetches.Load(), "exactly one fleet-wide refresh")
	})
}

func buildStatic(body string, ttl time.Duration, counter *atomic.Int32) func(context.Context) ([]byte, time.Duration, error) {
	return func(context.Context) ([]byte, time.Duration, error) {
		if counter != nil {
			counter.Add(1)
		}
		return []byte(body), ttl, nil
	}
}

func TestCachedBytesStaleRefreshClaimedOnceFleetWide(t *testing.T) {
	st := providertest.NewMemStore()
	podA, podB := core.NewCache(st), core.NewCache(st)
	var builds atomic.Int32
	ctx := context.Background()

	_, err := core.CachedBytes(ctx, podA, "k", nil, buildStatic(`{"n":1}`, 20*time.Millisecond, &builds))
	require.NoError(t, err)
	require.Equal(t, int32(1), builds.Load())
	time.Sleep(40 * time.Millisecond)

	release := make(chan struct{})
	rebuild := func(rctx context.Context) ([]byte, time.Duration, error) {
		<-release
		return buildStatic(`{"n":2}`, time.Minute, &builds)(rctx)
	}
	for _, pod := range []*core.Cache{podA, podB} {
		b, gerr := core.CachedBytes(ctx, pod, "k", nil, rebuild)
		require.NoError(t, gerr)
		assert.JSONEq(t, `{"n":1}`, string(b), "stale hit must serve the old bytes")
	}
	close(release)

	require.Eventually(t, func() bool {
		got, gerr := core.CachedBytes(ctx, podA, "k", nil, rebuild)
		return gerr == nil && string(got) == `{"n":2}`
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), builds.Load(), "one cold fill + exactly one fleet-wide refresh")
}

type namedFlavor struct {
	name   string
	flavor cacheFlavor
}

func protectedRows(typed, bytes string) []namedFlavor {
	return []namedFlavor{{typed, typedFlavor}, {bytes, bytesFlavor}}
}

func TestAdmitIsSkippedOnAFreshHit(t *testing.T) {
	for _, row := range protectedRows("TestCachedAdmitSkippedOnHit", "TestCachedBytesAdmitSkippedOnFreshHit") {
		t.Run(row.name, func(t *testing.T) {
			c := core.NewCache(providertest.NewMemStore())
			var fetches atomic.Int32
			_, err := row.flavor.get(c, "k", nil, fetchOf(&fetches, `"x"`, nil))
			require.NoError(t, err)

			got, err := row.flavor.get(c, "k", func(context.Context) error {
				t.Error("a fresh hit must not spend budget")
				return nil
			}, fetchOf(&fetches, `"y"`, nil))
			require.NoError(t, err)
			assert.Contains(t, got, "x")
		})
	}
}

func TestAdmitDenialIsNotCached(t *testing.T) {
	for _, row := range protectedRows("typed admit denial is not cached", "TestCachedBytesAdmitDenialIsNotCached") {
		t.Run(row.name, func(t *testing.T) {
			c := core.NewCache(providertest.NewMemStore())
			var fetches atomic.Int32
			denied := &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

			_, err := row.flavor.get(c, "k", func(context.Context) error { return denied }, fetchOf(&fetches, `"x"`, nil))
			require.ErrorIs(t, err, denied)
			assert.Zero(t, fetches.Load(), "a denied caller must never reach the upstream")

			got, err := row.flavor.get(c, "k", nil, fetchOf(&fetches, `"x"`, nil))
			require.NoError(t, err)
			assert.Contains(t, got, "x", "a denial must not poison the key")
		})
	}
}

func TestAdmitIsPerCallerUnderOneFlight(t *testing.T) {
	for _, row := range protectedRows("TestCachedAdmitIsPerCallerUnderOneFlight", "TestCachedBytesAdmitIsPerCallerUnderOneFlight") {
		t.Run(row.name, func(t *testing.T) {
			c := core.NewCache(providertest.NewMemStore())
			denied := &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}

			const perLane = 4
			var fetches atomic.Int32
			release := make(chan struct{})
			fill := func(context.Context) (string, error) {
				<-release
				fetches.Add(1)
				return `"x"`, nil
			}

			type outcome struct {
				body string
				err  error
			}
			premium, standard := make([]outcome, perLane), make([]outcome, perLane)
			var admitted, wg sync.WaitGroup
			admitted.Add(2 * perLane)
			fire := func(out []outcome, verdict error) {
				for i := range out {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						admit := func(context.Context) error {
							admitted.Done()
							return verdict
						}
						body, err := row.flavor.get(c, "k", admit, fill)
						out[i] = outcome{body, err}
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
				assert.Contains(t, got.body, "x")
			}
			for i, got := range standard {
				assert.ErrorIs(t, got.err, denied, "standard caller %d must be denied by its own lane", i)
			}
			assert.EqualValues(t, 1, fetches.Load(), "the flight must still cost one upstream call")
		})
	}
}

func TestCachedStoresUpstreamAbsenceButNeverThrottling(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         *core.UpstreamError
		wantFetches int32
	}{
		{"serves a 404 from the shared negative cache", &core.UpstreamError{Status: 404, Message: "player not found"}, 1},
		{"serves a 400 from the shared negative cache", &core.UpstreamError{Status: 400, Message: "bad name"}, 1},
		{"retries a 429 instead of caching it", &core.UpstreamError{Status: 429, Message: "busy"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := providertest.NewMemStore()
			var fetches atomic.Int32
			fetch := func(context.Context) (string, error) {
				fetches.Add(1)
				return "", tc.err
			}

			for _, replica := range []*core.Cache{core.NewCache(store), core.NewCache(store)} {
				_, err := typedFlavor.get(replica, "k", nil, fetch)
				var ue *core.UpstreamError
				require.ErrorAs(t, err, &ue)
				assert.Equal(t, tc.err.Status, ue.Status)
				assert.Equal(t, tc.err.Message, ue.Message)
			}
			assert.Equal(t, tc.wantFetches, fetches.Load())
		})
	}
}

func TestCachedNegativeIsNotRevalidated(t *testing.T) {
	c := core.NewCache(providertest.NewMemStore())
	var fetches atomic.Int32
	fetch := fetchOf(&fetches, "", &core.UpstreamError{Status: 404, Message: "player not found"})

	for range 3 {
		_, err := typedFlavor.get(c, "k", nil, fetch)
		require.Error(t, err)
	}
	assert.Never(t, func() bool { return fetches.Load() > 1 }, 50*time.Millisecond, 5*time.Millisecond,
		"a cached negative must not be refetched")
}

func TestCachedStoresAnEmptySuccess(t *testing.T) {
	c := core.NewCache(providertest.NewMemStore())
	var fetches atomic.Int32

	for range 2 {
		got, err := typedFlavor.get(c, "k", nil, fetchOf(&fetches, "", nil))
		require.NoError(t, err)
		assert.Empty(t, got)
	}
	assert.EqualValues(t, 1, fetches.Load(), "an empty-string success must be served from the cache")
}

func TestCachedRefreshesAnEntryWithoutAFreshnessStamp(t *testing.T) {
	store := providertest.NewMemStore()
	seed(t, store, `{"v":"old"}`)
	c := core.NewCache(store)
	var fetches atomic.Int32
	fetch := fetchOf(&fetches, "new", nil)

	got, err := typedFlavor.get(c, "k", nil, fetch)
	require.NoError(t, err)
	assert.Equal(t, "old", got, "the legacy value is still served, not discarded")

	require.Eventually(t, func() bool {
		v, gerr := typedFlavor.get(c, "k", nil, fetch)
		return gerr == nil && v == "new"
	}, time.Second, 10*time.Millisecond)
	assert.EqualValues(t, 1, fetches.Load())
}

func TestStoreCachedHydratesTheCache(t *testing.T) {
	notFound := &core.UpstreamError{Status: 404, Message: "player not found"}
	for _, tc := range []struct {
		name        string
		req         core.StoreRequest[string]
		want        string
		wantErr     error
		wantFetches int32
	}{
		{"serves a hydrated value as a hit", core.StoreRequest[string]{Value: "hydrated"}, "hydrated", nil, 0},
		{"serves a hydrated absence as a hit", core.StoreRequest[string]{Err: notFound}, "", notFound, 0},
		{"stores nothing for an infrastructure failure", core.StoreRequest[string]{Err: errors.New("exploded")}, "fetched", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := core.NewCache(providertest.NewMemStore())
			tc.req.Key, tc.req.TTL, tc.req.NegativeTTL = "k", time.Minute, time.Minute
			core.StoreCached(context.Background(), c, tc.req)
			var fetches atomic.Int32

			got, err := typedFlavor.get(c, "k", nil, fetchOf(&fetches, "fetched", nil))
			assert.Equal(t, tc.wantErr, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantFetches, fetches.Load())
		})
	}
}

func TestSnapshotsRoundTripThroughTheCache(t *testing.T) {
	c := core.NewCache(providertest.NewMemStore())
	type snapshot struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	require.NoError(t, c.SetJSON(context.Background(), "snap", snapshot{Name: "s", N: 3}, time.Hour))

	var got snapshot
	ok, err := c.GetJSON(context.Background(), "snap", &got)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, snapshot{Name: "s", N: 3}, got)

	ok, err = c.GetJSON(context.Background(), "missing", &got)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestCacheKeyFormats(t *testing.T) {
	assert.Equal(t, "gossip:urchin:daily:techno", core.Key("urchin", "daily", "techno"))

	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"  FrOsTy  ", "6"}, "frosty:6"},
		{[]string{"0", " Ca ", "predicted"}, "0:ca:predicted"},
		{[]string{"", "kr", "pc"}, ":kr:pc"},
		{[]string{"solo"}, "solo"},
	} {
		assert.Equal(t, tc.want, core.CacheID(tc.parts...), "parts %q", tc.parts)
	}
}

func BenchmarkCachedBytesHit(b *testing.B) {
	c := core.NewCache(providertest.NewMemStore())
	ctx := context.Background()
	_, err := core.CachedBytes(ctx, c, "k", nil, func(context.Context) ([]byte, time.Duration, error) {
		return []byte(`{"player":"Techno","wins":5,"losses":2}`), time.Hour, nil
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := core.CachedBytes(ctx, c, "k", nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}
