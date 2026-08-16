// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// NewTiered fronts inner with a bounded per-pod LRU claim cache. A same-pod
// redelivery burst (the common case: one pod holds the whole lane and the server
// resends the same stored message) is absorbed locally with zero network — a
// cache hit short-circuits Seen to "duplicate" without touching inner. A miss
// falls through to inner and records the resulting claim locally. The two tiers
// compose as a Store, so callers see one interface regardless of depth.
//
// capacity bounds the cache; entries also expire at their own ttl, so a claim
// never lingers past the idempotency window. A local eviction is safe: it only
// forces the next delivery of that key to consult inner, which is still
// authoritative.
func NewTiered(capacity int, inner Store) Store {
	return &lruStore{inner: inner, cache: newTTLLRU(capacity, time.Now)}
}

type lruStore struct {
	inner Store
	cache *ttlLRU
}

// claimKey is a caller's idempotency key as the local tier holds it. The cache
// is keyed on the UNPREFIXED key the caller passed, never on the prefix-composed
// string ValkeyStore writes; the two are the same width and the same underlying
// type, and mixing them would leave a local claim no lookup ever finds. Naming
// the local one is what stops the conversion from being silent.
type claimKey string

func (l *lruStore) Seen(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if l.cache.has(claimKey(key)) {
		return true, nil // already claimed on this pod
	}
	seen, err := l.inner.Seen(ctx, key, ttl)
	if err != nil {
		// inner failed open. Do NOT cache the miss: a retry must be free to claim
		// once the backend recovers, and caching would mask the outage.
		return seen, err
	}
	l.cache.add(claimKey(key), ttl)
	return seen, nil
}

func (l *lruStore) Release(ctx context.Context, key string) error {
	// Drop the local claim too, or a same-pod redelivery of the FAILED effect
	// would short-circuit to "duplicate" and never re-run.
	l.cache.remove(claimKey(key))
	return l.inner.Release(ctx, key)
}

// ttlLRU is a fixed-capacity, per-key TTL cache with LRU eviction. It is the
// per-pod tier's local claim set; it holds only keys (presence is the claim), so
// its footprint is bounded by capacity regardless of value size.
type ttlLRU struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	items map[claimKey]*list.Element
	now   func() time.Time
}

type lruEntry struct {
	key     claimKey
	expires time.Time
}

func newTTLLRU(capacity int, now func() time.Time) *ttlLRU {
	if capacity < 1 {
		capacity = 1
	}
	if now == nil {
		now = time.Now
	}
	return &ttlLRU{
		cap:   capacity,
		ll:    list.New(),
		items: make(map[claimKey]*list.Element, capacity),
		now:   now,
	}
}

// has reports whether key holds a live claim, dropping it if it has expired.
func (c *ttlLRU) has(key claimKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return false
	}
	if !c.now().Before(el.Value.(*lruEntry).expires) {
		c.drop(el)
		return false
	}
	c.ll.MoveToFront(el)
	return true
}

// add records a claim for ttl, refreshing an existing key in place.
func (c *ttlLRU) add(key claimKey, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	expires := c.now().Add(ttl)
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).expires = expires
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&lruEntry{key: key, expires: expires})
	c.items[key] = el
	if c.ll.Len() > c.cap {
		c.drop(c.ll.Back())
	}
}

func (c *ttlLRU) remove(key claimKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.drop(el)
	}
}

func (c *ttlLRU) drop(el *list.Element) {
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*lruEntry).key)
}

func (c *ttlLRU) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
