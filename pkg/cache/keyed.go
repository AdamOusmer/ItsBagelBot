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

type Keyed[K comparable, V any] struct {
	client *theine.Cache[K, entry[V]]
	group  singleflight.Group
	keyFn  func(K) string

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
		capacity:    capacity,
		ttl:         ttl,
		staleWindow: o.staleWindow,
	}
}

func (c *Keyed[K, V]) Len() int { return c.client.Len() }

func (c *Keyed[K, V]) Capacity() int64 { return c.capacity }

// Get returns a live cached entry without loading a miss. Fill misses with
// GetOrLoad so concurrent readers retain singleflight protection.
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

	result, err, _ := c.group.Do(c.keyFn(key), func() (any, error) {
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

	value, ttl, err := loader(ctx)
	if err != nil {
		return c.staleOr(key, value, err)
	}

	c.SetFor(key, value, ttl)
	return value, nil
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
	ttl += rand.N(ttl/10 + 1)
	e := entry[V]{value: value}
	if c.staleWindow > 0 {
		e.freshUntil = time.Now().Add(ttl)
	}
	c.client.SetWithTTL(key, e, 1, ttl+c.staleWindow)
}

func (c *Keyed[K, V]) Invalidate(key K) {
	c.group.Forget(c.keyFn(key))
	c.client.Delete(key)
}

func (c *Keyed[K, V]) Close() {
	c.client.Close()
}
