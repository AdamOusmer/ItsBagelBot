// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/Yiling-J/theine-go"
	"golang.org/x/sync/singleflight"
)

// Keyed is the in-process TTL cache every cache in this package is: a theine
// store keyed by an arbitrary comparable K, with concurrent misses on one key
// collapsed into a single loader call so a cold or invalidated key can never
// stampede the database. Cache is this type with K = string.
//
// On the hot path (a cache hit) it hashes K directly, so a caller holding a
// non-string key (e.g. a uint64 broadcaster id) pays no string allocation per
// read. singleflight is string-keyed, so K is stringified by keyFn ONLY on the
// miss and invalidate paths, never on a hit. Expirations are jittered so entries
// written together do not all expire together.
type Keyed[K comparable, V any] struct {
	client *theine.Cache[K, V]
	group  singleflight.Group
	keyFn  func(K) string

	capacity int64
	ttl      time.Duration
	jitter   time.Duration
}

// NewKeyed creates a K-keyed cache. keyFn maps a key to the stable string
// singleflight uses to collapse concurrent misses; it is called only on miss and
// invalidate, never on a hit. Call Close when the cache is no longer needed.
func NewKeyed[K comparable, V any](capacity int64, ttl time.Duration, keyFn func(K) string) *Keyed[K, V] {
	client, err := theine.NewBuilder[K, V](capacity).Build()
	if err != nil {
		panic("failed to build theine cache: " + err.Error())
	}
	return &Keyed[K, V]{
		client:   client,
		keyFn:    keyFn,
		capacity: capacity,
		ttl:      ttl,
		jitter:   ttl / 10,
	}
}

// Len returns the current number of live entries in the cache, a point-in-time
// occupancy reading for logging how full the cache runs against its capacity.
func (c *Keyed[K, V]) Len() int { return c.client.Len() }

// Capacity returns the configured maximum number of entries (the ceiling passed
// to NewKeyed), the denominator for an occupancy ratio.
func (c *Keyed[K, V]) Capacity() int64 { return c.capacity }

// GetOrLoad returns the cached value for key, or runs loader to fill it and
// stores the result for the cache's configured ttl. A hit touches only theine's
// Get (no allocation, no keyFn). Only one loader runs per key at a time; the
// others wait and share the result.
//
// The TTL-carrying wrapper is built on every call rather than behind a hit
// fast-path: measured on an M1 Pro, a hit costs 16 B/op and 1 alloc/op with and
// without that fast-path (the allocation is theine's read tracking, and escape
// analysis keeps this closure off the heap), so the fast path bought a second
// copy of the lookup and nothing else.
func (c *Keyed[K, V]) GetOrLoad(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	return c.GetOrLoadTTL(ctx, key, func(ctx context.Context) (V, time.Duration, error) {
		value, err := loader(ctx)
		return value, c.ttl, err
	})
}

// GetOrLoadTTL is GetOrLoad with a loader-selected TTL. It is useful when
// positive and negative results need different freshness windows. Failed loads
// are never cached, and concurrent misses remain singleflight-collapsed.
func (c *Keyed[K, V]) GetOrLoadTTL(ctx context.Context, key K, loader func(context.Context) (V, time.Duration, error)) (V, error) {
	if value, ok := c.client.Get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(c.keyFn(key), func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if value, ok := c.client.Get(key); ok {
			return value, nil
		}

		value, ttl, err := loader(ctx)
		if err != nil {
			return value, err
		}

		c.SetFor(key, value, ttl)
		return value, nil
	})

	if err != nil {
		var zero V
		return zero, err
	}

	return result.(V), nil
}

// Set stores value under key with a jittered TTL.
func (c *Keyed[K, V]) Set(key K, value V) {
	c.client.SetWithTTL(key, value, 1, c.ttl+rand.N(c.jitter+1))
}

// SetFor stores value with a caller-selected TTL and the same 0-10% expiry
// jitter as Set.
func (c *Keyed[K, V]) SetFor(key K, value V, ttl time.Duration) {
	jitter := ttl / 10
	c.client.SetWithTTL(key, value, 1, ttl+rand.N(jitter+1))
}

// Invalidate drops key and forgets any in-flight load for it, so the next read
// observes the new state instead of a stale flight result.
func (c *Keyed[K, V]) Invalidate(key K) {
	c.group.Forget(c.keyFn(key))
	c.client.Delete(key)
}

// Close closes the underlying theine cache.
func (c *Keyed[K, V]) Close() {
	c.client.Close()
}
