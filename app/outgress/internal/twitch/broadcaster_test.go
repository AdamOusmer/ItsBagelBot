package twitch

import (
	"strconv"
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
