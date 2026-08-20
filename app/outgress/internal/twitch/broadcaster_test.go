// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// countingBuild returns a build func that records how many times it ran and
// hands back a distinct *Source per call, so tests can tell a cache reuse from
// a rebuild without any network.
func countingBuild(calls *int) func(string) *Source {
	return func(string) *Source {
		*calls++
		return &Source{}
	}
}

// TestGetBuildsOncePerBroadcaster pins the "refresh still works for active
// broadcasters" criterion: a hot channel builds its Source once and reuses it
// on every later send, while a distinct channel gets its own Source.
func TestGetBuildsOncePerBroadcaster(t *testing.T) {
	var calls int
	b := NewBroadcasterTokens(countingBuild(&calls))

	first := b.Get("chan-a")
	second := b.Get("chan-a")
	if first == nil || first != second {
		t.Fatalf("cache hit returned a different Source: %p vs %p", first, second)
	}
	if calls != 1 {
		t.Fatalf("build ran %d times for one broadcaster, want 1", calls)
	}

	if third := b.Get("chan-b"); third == first {
		t.Fatal("distinct broadcaster shared a Source")
	}
	if calls != 2 {
		t.Fatalf("build ran %d times for two broadcasters, want 2", calls)
	}
}

// TestGetNilReceiverAndEmptyID guards the early return that lets callers treat a
// disabled cache and a missing broadcaster id uniformly, returning a nil Source
// (send as bot / error at send time) rather than panicking.
func TestGetNilReceiverAndEmptyID(t *testing.T) {
	var nilCache *BroadcasterTokens
	if got := nilCache.Get("chan-a"); got != nil {
		t.Fatalf("nil-receiver Get = %p, want nil", got)
	}

	b := NewBroadcasterTokens(countingBuild(new(int)))
	if got := b.Get(""); got != nil {
		t.Fatalf("empty-id Get = %p, want nil", got)
	}
}

// TestEvictLockedExpiresIdleEntries covers the TTL half of eviction: an entry
// untouched past sourceIdleTTL is dropped while one used within the window
// stays. Driving evictLocked directly keeps this deterministic instead of
// filling the cache to its cap and sleeping an hour.
func TestEvictLockedExpiresIdleEntries(t *testing.T) {
	b := NewBroadcasterTokens(countingBuild(new(int)))
	now := time.Now()

	b.Get("idle")
	b.Get("active")
	b.cache["idle"].lastUsed = now.Add(-sourceIdleTTL - time.Minute)
	b.cache["active"].lastUsed = now.Add(-time.Minute)

	b.mu.Lock()
	b.evictLocked(now)
	b.mu.Unlock()

	if _, ok := b.cache["idle"]; ok {
		t.Error("entry idle past sourceIdleTTL survived eviction")
	}
	if _, ok := b.cache["active"]; !ok {
		t.Error("entry used within sourceIdleTTL was evicted")
	}
}

// TestGetEvictsIdleEntryAtCapacity pins the bounded-resident-set criterion:
// inserting a new broadcaster into a full cache must not grow it past the cap,
// and an entry idle past its TTL is the one released.
func TestGetEvictsIdleEntryAtCapacity(t *testing.T) {
	b := NewBroadcasterTokens(countingBuild(new(int)))
	for i := range maxBroadcasterSources {
		b.Get(strconv.Itoa(i))
	}
	if len(b.cache) != maxBroadcasterSources {
		t.Fatalf("cache filled to %d, want %d", len(b.cache), maxBroadcasterSources)
	}
	// Mark "0" idle so the TTL sweep, not the LRU fallback, selects it.
	b.cache["0"].lastUsed = time.Now().Add(-sourceIdleTTL - time.Minute)

	b.Get("overflow")

	if len(b.cache) > maxBroadcasterSources {
		t.Fatalf("cache grew to %d past cap %d", len(b.cache), maxBroadcasterSources)
	}
	if _, ok := b.cache["0"]; ok {
		t.Error("idle entry was not evicted on insert-at-cap")
	}
	if _, ok := b.cache["overflow"]; !ok {
		t.Error("new broadcaster missing after insert-at-cap")
	}
}

// TestGetEvictsLeastRecentlyUsedWhenNoneIdle covers the LRU fallback: with the
// cache full and every entry still inside its TTL, the insert cannot exceed the
// cap, so the least recently used entry is dropped instead.
func TestGetEvictsLeastRecentlyUsedWhenNoneIdle(t *testing.T) {
	b := NewBroadcasterTokens(countingBuild(new(int)))
	for i := range maxBroadcasterSources {
		b.Get(strconv.Itoa(i))
	}
	// Every entry is fresh; make "7" strictly the oldest without crossing TTL.
	const lru = "7"
	b.cache[lru].lastUsed = time.Now().Add(-time.Minute)

	b.Get("overflow")

	if len(b.cache) > maxBroadcasterSources {
		t.Fatalf("cache grew to %d past cap %d", len(b.cache), maxBroadcasterSources)
	}
	if _, ok := b.cache[lru]; ok {
		t.Errorf("least-recently-used entry %q survived eviction", lru)
	}
	if _, ok := b.cache["overflow"]; !ok {
		t.Error("new broadcaster missing after LRU eviction")
	}
}

// nearExpirySource builds a Source whose cached token is within
// refreshMargin of expiry -- i.e. due for renewal -- with refresh wired to
// fn so tests can observe whether a sweep actually called it.
func nearExpirySource(fn func(context.Context) (string, time.Duration, error)) *Source {
	s := &Source{refresh: fn}
	s.mu.Lock()
	s.token = "stale"
	s.expires = time.Now().Add(refreshMargin - time.Second)
	s.mu.Unlock()
	return s
}

// TestSweepOnceRefreshesOrSkipsSource covers both ends of sweepOnce's
// per-entry decision from one shared scaffold: a near-expiry source in the
// cache (the reason this sweep exists -- it gets renewed through the same
// refreshIfDue path token.go's per-Source background refresher uses) versus
// one evicted from the cache before the sweep runs (which sweepOnce must
// never touch again -- see StartRefreshSweep's "eviction is free" doc,
// which depends on a single ticker rather than one per Source).
func TestSweepOnceRefreshesOrSkipsSource(t *testing.T) {
	cases := []struct {
		name      string
		evict     bool
		wantCalls int32
	}{
		{name: "near-expiry source in cache is refreshed", evict: false, wantCalls: 1},
		{name: "source evicted before sweep is left alone", evict: true, wantCalls: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			b := NewBroadcasterTokens(func(string) *Source {
				return nearExpirySource(func(context.Context) (string, time.Duration, error) {
					atomic.AddInt32(&calls, 1)
					return "fresh", time.Hour, nil
				})
			})
			b.Get("chan-a")

			if tc.evict {
				b.mu.Lock()
				delete(b.cache, "chan-a")
				b.mu.Unlock()
			}

			b.sweepOnce(context.Background())

			if got := atomic.LoadInt32(&calls); got != tc.wantCalls {
				t.Fatalf("refresh calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// TestSweepOnceLeavesHealthySourceAlone covers the flip side: a source whose
// token is nowhere near expiry must not trigger a refresh call, or the sweep
// would mint far more often than refreshMargin ever requires.
func TestSweepOnceLeavesHealthySourceAlone(t *testing.T) {
	var calls int32
	b := NewBroadcasterTokens(func(string) *Source {
		s := &Source{refresh: func(context.Context) (string, time.Duration, error) {
			atomic.AddInt32(&calls, 1)
			return "fresh", time.Hour, nil
		}}
		s.mu.Lock()
		s.token = "healthy"
		s.expires = time.Now().Add(2 * time.Hour)
		s.mu.Unlock()
		return s
	})
	b.Get("chan-a")

	b.sweepOnce(context.Background())

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("refresh calls = %d, want 0 for a healthy token", got)
	}
}

// TestSweepOnceDoesNotExtendLastUsed guards against a sweep quietly
// defeating sourceIdleTTL: if sweeping counted as use, an otherwise-idle
// broadcaster would never go idle long enough to be evicted.
func TestSweepOnceDoesNotExtendLastUsed(t *testing.T) {
	b := NewBroadcasterTokens(func(string) *Source {
		return nearExpirySource(func(context.Context) (string, time.Duration, error) {
			return "fresh", time.Hour, nil
		})
	})
	b.Get("chan-a")
	old := time.Now().Add(-sourceIdleTTL - time.Minute)
	b.cache["chan-a"].lastUsed = old

	b.sweepOnce(context.Background())

	if got := b.cache["chan-a"].lastUsed; !got.Equal(old) {
		t.Fatalf("sweep changed lastUsed to %v, want unchanged %v", got, old)
	}
}

// TestSweepOnceDoesNotHoldLockDuringRefresh is the correctness property the
// whole design turns on: sweepOnce must release b.mu before running any
// refresh, because refresh performs NATS RPC and HTTP I/O and Get (which
// also takes b.mu) sits on the send path of every broadcaster-identity
// message the process handles. The fake refresh here calls b.Get from
// inside the refresh call itself; if sweepOnce still held the lock at that
// point, this would deadlock (b.mu is not reentrant) instead of returning.
func TestSweepOnceDoesNotHoldLockDuringRefresh(t *testing.T) {
	var b *BroadcasterTokens
	b = NewBroadcasterTokens(func(string) *Source {
		return nearExpirySource(func(context.Context) (string, time.Duration, error) {
			b.Get("other-chan")
			return "fresh", time.Hour, nil
		})
	})
	b.Get("chan-a")

	done := make(chan struct{})
	go func() {
		b.sweepOnce(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sweepOnce deadlocked: b.mu was still held while refresh ran")
	}
}
