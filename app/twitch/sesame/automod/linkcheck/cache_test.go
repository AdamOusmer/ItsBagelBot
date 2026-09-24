// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"fmt"
	"testing"
	"time"
)

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

	advance(shortTTL + time.Minute)
	requireExpired(t, c, "bit.ly/abc")
	requireCached(t, c, "ok.example", Clean)
	requireCached(t, c, "bad.example", Bad)

	advance(cleanTTL - shortTTL)
	requireExpired(t, c, "ok.example")
	requireCached(t, c, "bad.example", Bad)

	advance(badTTL - cleanTTL)
	requireExpired(t, c, "bad.example")
}

func requireCached(t *testing.T, c *cache, key string, want Verdict) {
	t.Helper()
	if v, ok := c.get(key); !ok || v != want {
		t.Fatalf("get(%q) = (%v,%v), want (%v,true)", key, v, ok, want)
	}
}

func requireExpired(t *testing.T, c *cache, key string) {
	t.Helper()
	if _, ok := c.get(key); ok {
		t.Fatalf("get(%q) still cached, want expired", key)
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

	c.put("hot.example", Bad, false)
	if v, ok := c.get("hot.example"); !ok || v != Bad {
		t.Fatalf("re-put after eviction = (%v,%v)", v, ok)
	}
}
