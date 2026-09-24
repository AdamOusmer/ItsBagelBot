// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"container/list"
	"context"
	"sync"
	"time"
)

func NewTiered(capacity int, inner Store) Store {
	return &lruStore{inner: inner, cache: newTTLLRU(capacity, time.Now)}
}

type lruStore struct {
	inner Store
	cache *ttlLRU
}

type claimKey string

func (l *lruStore) Seen(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if l.cache.has(claimKey(key)) {
		return true, nil
	}
	// Never cache a fail-open miss: a retry must be free to claim once the backend recovers.
	seen, err := l.inner.Seen(ctx, key, ttl)
	if err != nil {
		return seen, err
	}
	l.cache.add(claimKey(key), ttl)
	return seen, nil
}

func (l *lruStore) Release(ctx context.Context, key string) error {
	// Drop the local claim too, or a same-pod redelivery short-circuits and never re-runs.
	l.cache.remove(claimKey(key))
	return l.inner.Release(ctx, key)
}

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
