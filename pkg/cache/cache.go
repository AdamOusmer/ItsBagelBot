package cache

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/Yiling-J/theine-go"
	"golang.org/x/sync/singleflight"
)

// DefaultCapacity is the default maximum number of entries for a cache, used
// when a caller has no reason to size the cache differently.
const DefaultCapacity int64 = 10000

// Cache is an in-process TTL cache wrapper around theine-go. Concurrent misses
// on the same key are collapsed into a single loader call through singleflight,
// so a cold or invalidated key can never stampede the database. Expirations are
// jittered so entries written together do not all expire together.
type Cache[V any] struct {
	client *theine.Cache[string, V]
	group  singleflight.Group

	ttl    time.Duration
	jitter time.Duration
}

// New creates a cache with a maximum capacity whose entries live for ttl plus
// a random jitter in [0, ttl/10). Theine automatically evicts items when full or expired.
// Call Close when the cache is no longer needed.
func New[V any](capacity int64, ttl time.Duration) *Cache[V] {
	client, err := theine.NewBuilder[string, V](capacity).Build()
	if err != nil {
		panic("failed to build theine cache: " + err.Error())
	}

	return &Cache[V]{
		client: client,
		ttl:    ttl,
		jitter: ttl / 10,
	}
}

// GetOrLoad returns the cached value for key, or runs loader to fill it.
// Only one loader runs per key at a time, regardless of how many goroutines
// miss concurrently; the others wait and share the same result.
func (c *Cache[V]) GetOrLoad(ctx context.Context, key string, loader func(context.Context) (V, error)) (V, error) {
	if value, ok := c.client.Get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(key, func() (any, error) {
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
func (c *Cache[V]) Set(key string, value V) {
	jitteredTTL := c.ttl + rand.N(c.jitter+1)
	c.client.SetWithTTL(key, value, 1, jitteredTTL)
}

// Invalidate drops key from the cache and forgets any in-flight load for it,
// so the next read observes the new state instead of a stale flight result.
func (c *Cache[V]) Invalidate(key string) {
	c.group.Forget(key)
	c.client.Delete(key)
}

// Close closes the underlying theine cache.
func (c *Cache[V]) Close() {
	c.client.Close()
}
