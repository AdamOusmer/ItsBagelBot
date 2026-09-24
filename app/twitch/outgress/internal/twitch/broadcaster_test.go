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

func countingBuild(calls *int) func(string) *Source {
	return func(string) *Source {
		*calls++
		return &Source{}
	}
}

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

func TestGetEvictsIdleEntryAtCapacity(t *testing.T) {
	b := NewBroadcasterTokens(countingBuild(new(int)))
	for i := range maxBroadcasterSources {
		b.Get(strconv.Itoa(i))
	}
	if len(b.cache) != maxBroadcasterSources {
		t.Fatalf("cache filled to %d, want %d", len(b.cache), maxBroadcasterSources)
	}
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

func TestGetEvictsLeastRecentlyUsedWhenNoneIdle(t *testing.T) {
	b := NewBroadcasterTokens(countingBuild(new(int)))
	for i := range maxBroadcasterSources {
		b.Get(strconv.Itoa(i))
	}
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

func nearExpirySource(fn func(context.Context) (string, time.Duration, error)) *Source {
	s := &Source{refresh: fn}
	s.mu.Lock()
	s.token = "stale"
	s.expires = time.Now().Add(refreshMargin - time.Second)
	s.mu.Unlock()
	return s
}

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

func mintingSource(calls *int32) *Source {
	return &Source{refresh: func(context.Context) (string, time.Duration, error) {
		atomic.AddInt32(calls, 1)
		return "fresh", time.Hour, nil
	}}
}

const sweepPassesUnderTest = 3

func TestSweepRefreshesOnlyLiveTokens(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		expiresIn time.Duration
		wantCalls int32
	}{
		{name: "grant revoked or never given", token: "", expiresIn: refreshMargin - time.Second, wantCalls: 0},
		{name: "token already expired", token: "lapsed", expiresIn: -time.Minute, wantCalls: 0},
		{name: "live token inside the margin", token: "stale", expiresIn: refreshMargin - time.Second, wantCalls: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			s := mintingSource(&calls)
			s.token = tc.token
			s.expires = time.Now().Add(tc.expiresIn)

			b := NewBroadcasterTokens(func(string) *Source { return s })
			b.Get("chan-a")
			for range sweepPassesUnderTest {
				b.sweepOnce(context.Background())
			}

			got := atomic.LoadInt32(&calls)
			if got != tc.wantCalls {
				t.Fatalf("refresh calls across %d passes = %d, want %d", sweepPassesUnderTest, got, tc.wantCalls)
			}
		})
	}
}

func TestSweepTickEvictsIdleSourceBelowCap(t *testing.T) {
	b := NewBroadcasterTokens(func(string) *Source { return mintingSource(new(int32)) })
	b.Get("idle")
	b.Get("active")
	b.cache["idle"].lastUsed = time.Now().Add(-sourceIdleTTL - time.Minute)

	b.sweepTick(context.Background())

	if len(b.cache) >= maxBroadcasterSources {
		t.Fatalf("cache size %d is at the cap, so this proves nothing about the TTL", len(b.cache))
	}
	if _, ok := b.cache["idle"]; ok {
		t.Error("source idle past sourceIdleTTL survived a sweep tick")
	}
	if _, ok := b.cache["active"]; !ok {
		t.Error("source used within sourceIdleTTL was evicted by a sweep tick")
	}
}
