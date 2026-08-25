// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"fmt"
	"testing"
	"time"
)

// pinTime overrides nowNanos for the test and restores it on cleanup,
// returning a setter that advances the fake clock.
func pinTime(t *testing.T) func(advance time.Duration) {
	t.Helper()
	now := time.Now()
	nowNanos = func() int64 { return now.UnixNano() }
	t.Cleanup(func() { nowNanos = func() int64 { return time.Now().UnixNano() } })
	return func(d time.Duration) { now = now.Add(d) }
}

func TestCacheTTLs(t *testing.T) {
	advance := pinTime(t)
	c := newCache()

	c.put("bad.example", Bad, false)
	c.put("ok.example", Clean, false)
	c.put("bit.ly/abc", Clean, true)

	// +1h: the shortener slot lapses first (destinations rotate server-side);
	// host slots hold.
	advance(shortTTL + time.Minute)
	if _, ok := c.get("bit.ly/abc"); ok {
		t.Fatal("shortener entry survived shortTTL")
	}
	if v, ok := c.get("ok.example"); !ok || v != Clean {
		t.Fatalf("clean entry expired before cleanTTL: (%v,%v)", v, ok)
	}
	if v, ok := c.get("bad.example"); !ok || v != Bad {
		t.Fatalf("bad entry expired early: (%v,%v)", v, ok)
	}

	// +6h: clean lapses, bad still holds its day.
	advance(cleanTTL - shortTTL)
	if _, ok := c.get("ok.example"); ok {
		t.Fatal("clean entry survived its TTL")
	}
	if v, ok := c.get("bad.example"); !ok || v != Bad {
		t.Fatalf("bad entry did not survive past cleanTTL: (%v,%v)", v, ok)
	}

	// +24h: bad lapses too.
	advance(badTTL - cleanTTL)
	if _, ok := c.get("bad.example"); ok {
		t.Fatal("bad entry outlived badTTL")
	}
}

func TestCacheCapEvictsAndSelfHeals(t *testing.T) {
	pinTime(t)
	c := newCache()

	for i := 0; c.m != nil && i < maxEntries+512; i++ {
		c.put(fmt.Sprintf("host-%d.example", i), Clean, false)
	}
	if len(c.m) > maxEntries {
		t.Fatalf("cache grew past cap: %d", len(c.m))
	}

	// Re-inserting an evicted hot host re-caches it: eviction is not a ban list.
	c.put("hot.example", Bad, false)
	if v, ok := c.get("hot.example"); !ok || v != Bad {
		t.Fatalf("re-put after eviction = (%v,%v)", v, ok)
	}
}
