// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import (
	"context"
	"hash/maphash"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Yiling-J/theine-go"
	"golang.org/x/sync/singleflight"
)

type Option func(*options)

type options struct {
	staleWindow time.Duration
}

func StaleOnError(window time.Duration) Option {
	return func(o *options) { o.staleWindow = window }
}

type entry[V any] struct {
	value      V
	freshUntil time.Time
}

func (e *entry[V]) fresh() bool {
	return e.freshUntil.IsZero() || time.Now().Before(e.freshUntil)
}

const generationStripes = 256

type Keyed[K comparable, V any] struct {
	client  *theine.Cache[K, entry[V]]
	group   singleflight.Group
	keyFn   func(K) string
	mu      sync.Mutex
	seed    maphash.Seed
	epoch   atomic.Uint64
	stripes [generationStripes]atomic.Uint64
	loading map[K]int

	capacity    int64
	ttl         time.Duration
	staleWindow time.Duration
}

func NewKeyed[K comparable, V any](capacity int64, ttl time.Duration, keyFn func(K) string, opts ...Option) *Keyed[K, V] {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	client, err := theine.NewBuilder[K, entry[V]](capacity).Build()
	if err != nil {
		panic("failed to build theine cache: " + err.Error())
	}
	return &Keyed[K, V]{
		client:      client,
		keyFn:       keyFn,
		seed:        maphash.MakeSeed(),
		loading:     make(map[K]int),
		capacity:    capacity,
		ttl:         ttl,
		staleWindow: o.staleWindow,
	}
}

func (c *Keyed[K, V]) Len() int { return c.client.Len() }

func (c *Keyed[K, V]) Capacity() int64 { return c.capacity }

func (c *Keyed[K, V]) Get(key K) (V, bool) {
	if e, ok := c.client.Get(key); ok && e.fresh() {
		return e.value, true
	}
	var zero V
	return zero, false
}

func (c *Keyed[K, V]) GetOrLoad(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	return c.GetOrLoadTTL(ctx, key, func(ctx context.Context) (V, time.Duration, error) {
		value, err := loader(ctx)
		return value, c.ttl, err
	})
}

func (c *Keyed[K, V]) GetOrLoadTTL(ctx context.Context, key K, loader func(context.Context) (V, time.Duration, error)) (V, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	name := c.keyFn(key)
	flight := strconv.FormatUint(c.generation(name), 10) + ":" + name
	result, err, _ := c.group.Do(flight, func() (any, error) {
		return c.load(ctx, key, loader)
	})

	if err != nil {
		var zero V
		return zero, err
	}

	return result.(V), nil
}

func (c *Keyed[K, V]) load(ctx context.Context, key K, loader func(context.Context) (V, time.Duration, error)) (V, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	name := c.keyFn(key)
	c.mu.Lock()
	c.loading[key]++
	generation := c.generation(name)
	c.mu.Unlock()
	defer c.finishedLoad(key)
	value, ttl, err := loader(ctx)
	if err != nil {
		return c.staleOr(key, value, err)
	}

	c.mu.Lock()
	if generation == c.generation(name) {
		c.setLoaded(key, value, ttl)
	}
	c.mu.Unlock()
	return value, nil
}

func (c *Keyed[K, V]) finishedLoad(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loading[key]--
	if c.loading[key] == 0 {
		delete(c.loading, key)
	}
}

func (c *Keyed[K, V]) stripe(name string) *atomic.Uint64 {
	return &c.stripes[maphash.String(c.seed, name)%generationStripes]
}

func (c *Keyed[K, V]) generation(name string) uint64 {
	return c.epoch.Load() + c.stripe(name).Load()
}

func (c *Keyed[K, V]) staleOr(key K, value V, err error) (V, error) {
	if c.staleWindow <= 0 {
		return value, err
	}
	if e, ok := c.client.Get(key); ok {
		return e.value, nil
	}
	return value, err
}

func (c *Keyed[K, V]) Set(key K, value V) {
	c.SetFor(key, value, c.ttl)
}

func (c *Keyed[K, V]) SetFor(key K, value V, ttl time.Duration) {
	c.store(key, value, ttl+rand.N(ttl/10+1))
}

func (c *Keyed[K, V]) setLoaded(key K, value V, ttl time.Duration) {
	if ttl < c.ttl {
		c.store(key, value, ttl)
		return
	}
	c.SetFor(key, value, ttl)
}

func (c *Keyed[K, V]) store(key K, value V, ttl time.Duration) {
	e := entry[V]{value: value}
	if c.staleWindow > 0 {
		e.freshUntil = time.Now().Add(ttl)
	}
	c.client.SetWithTTL(key, e, 1, ttl+c.staleWindow)
}

func (c *Keyed[K, V]) Invalidate(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stripe(c.keyFn(key)).Add(1)
	c.client.Delete(key)
}

func (c *Keyed[K, V]) InvalidateWhere(match func(K) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	stale := make(map[K]struct{})
	c.client.Range(func(key K, _ entry[V]) bool {
		if match(key) {
			stale[key] = struct{}{}
		}
		return true
	})
	for key := range c.loading {
		if match(key) {
			stale[key] = struct{}{}
		}
	}
	for key := range stale {
		c.stripe(c.keyFn(key)).Add(1)
		c.client.Delete(key)
	}
}

func (c *Keyed[K, V]) Clear() {
	c.epoch.Add(1)
	c.InvalidateWhere(func(K) bool { return true })
}

type Event struct {
	Scope string
	ID    uint64
	Keys  []string
}

type Feed interface {
	Watch(scope string, fn func(Event))
	WatchGap(fn func())
}

func (c *Keyed[K, V]) InvalidateOn(f Feed, keys func(Event) []K, scopes ...string) *Keyed[K, V] {
	if f == nil {
		return c
	}
	for _, scope := range scopes {
		f.Watch(scope, func(e Event) {
			for _, key := range keys(e) {
				c.Invalidate(key)
			}
		})
	}
	f.WatchGap(c.Clear)
	return c
}

func (c *Keyed[K, V]) Close() {
	c.client.Close()
}
