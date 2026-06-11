package cache

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const shardCount = 16 // power of two, so the shard pick is a single AND

type entry[V any] struct {
	value     V
	expiresAt int64 // unix nanoseconds
}

type shard[V any] struct {
	mu      sync.RWMutex
	entries map[string]entry[V]
}

// Cache is a sharded in-process TTL cache. Concurrent misses on the same key
// are collapsed into a single loader call through singleflight, so a cold or
// invalidated key can never stampede the database. Expirations are jittered
// so entries written together do not all expire together.
type Cache[V any] struct {
	shards [shardCount]shard[V]
	group  singleflight.Group

	ttl    time.Duration
	jitter time.Duration

	stop chan struct{}
}

// New creates a cache whose entries live for ttl plus a random jitter in
// [0, ttl/10). A background sweeper evicts expired entries so memory is
// reclaimed even for keys that are never read again; call Close to stop it.
func New[V any](ttl time.Duration) *Cache[V] {

	c := &Cache[V]{
		ttl:    ttl,
		jitter: ttl / 10,
		stop:   make(chan struct{}),
	}

	for i := range c.shards {
		c.shards[i].entries = make(map[string]entry[V])
	}

	go c.sweep()

	return c
}

// GetOrLoad returns the cached value for key, or runs loader to fill it.
// Only one loader runs per key at a time, regardless of how many goroutines
// miss concurrently; the others wait and share the same result.
func (c *Cache[V]) GetOrLoad(ctx context.Context, key string, loader func(context.Context) (V, error)) (V, error) {

	if value, ok := c.get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(key, func() (any, error) {

		// A previous flight may have filled the key while we queued.
		if value, ok := c.get(key); ok {
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

	s := c.shardFor(key)

	expiresAt := time.Now().Add(c.ttl + rand.N(c.jitter+1)).UnixNano()

	s.mu.Lock()
	s.entries[key] = entry[V]{value: value, expiresAt: expiresAt}
	s.mu.Unlock()
}

// Invalidate drops key from the cache and forgets any in-flight load for it,
// so the next read observes the new state instead of a stale flight result.
func (c *Cache[V]) Invalidate(key string) {

	c.group.Forget(key)

	s := c.shardFor(key)

	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
}

// Close stops the background sweeper.
func (c *Cache[V]) Close() {
	close(c.stop)
}

func (c *Cache[V]) get(key string) (V, bool) {

	s := c.shardFor(key)
	now := time.Now().UnixNano()

	s.mu.RLock()
	e, ok := s.entries[key]
	s.mu.RUnlock()

	if !ok || now > e.expiresAt {
		var zero V
		return zero, false
	}

	return e.value, true
}

func (c *Cache[V]) shardFor(key string) *shard[V] {
	return &c.shards[fnv1a(key)&(shardCount-1)]
}

func (c *Cache[V]) sweep() {

	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
		}

		now := time.Now().UnixNano()

		for i := range c.shards {
			s := &c.shards[i]

			s.mu.Lock()
			for key, e := range s.entries {
				if now > e.expiresAt {
					delete(s.entries, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// fnv1a hashes the key without allocating.
func fnv1a(key string) uint32 {

	const (
		offset = 2166136261
		prime  = 16777619
	)

	hash := uint32(offset)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= prime
	}

	return hash
}
