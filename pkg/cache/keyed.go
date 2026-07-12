package cache

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/Yiling-J/theine-go"
	"golang.org/x/sync/singleflight"
)

// Keyed is an in-process TTL cache like Cache, but keyed by an arbitrary
// comparable K instead of a string. On the hot path (a cache hit) it hashes K
// directly, so a caller holding a non-string key (e.g. a uint64 broadcaster id)
// pays no string allocation per read, which Cache[string] cannot avoid.
//
// Concurrent misses on the same key collapse through singleflight exactly like
// Cache. singleflight is string-keyed, so K is stringified by keyFn ONLY on the
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

// GetOrLoad returns the cached value for key, or runs loader to fill it. A hit
// touches only theine's Get (no allocation, no keyFn). Only one loader runs per
// key at a time; the others wait and share the result.
func (c *Keyed[K, V]) GetOrLoad(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	if value, ok := c.client.Get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(c.keyFn(key), func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if value, ok := c.client.Get(key); ok {
			return value, nil
		}

		value, err := loader(ctx)
		if err != nil {
			return value, err
		}

		c.Set(key, value)
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
