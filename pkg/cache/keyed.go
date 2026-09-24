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

type Keyed[K comparable, V any] struct {
	client *theine.Cache[K, V]
	group  singleflight.Group
	keyFn  func(K) string

	capacity int64
	ttl      time.Duration
	jitter   time.Duration
}

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

func (c *Keyed[K, V]) Len() int { return c.client.Len() }

func (c *Keyed[K, V]) Capacity() int64 { return c.capacity }

func (c *Keyed[K, V]) GetOrLoad(ctx context.Context, key K, loader func(context.Context) (V, error)) (V, error) {
	return c.GetOrLoadTTL(ctx, key, func(ctx context.Context) (V, time.Duration, error) {
		value, err := loader(ctx)
		return value, c.ttl, err
	})
}

func (c *Keyed[K, V]) GetOrLoadTTL(ctx context.Context, key K, loader func(context.Context) (V, time.Duration, error)) (V, error) {
	if value, ok := c.client.Get(key); ok {
		return value, nil
	}

	result, err, _ := c.group.Do(c.keyFn(key), func() (any, error) {
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

func (c *Keyed[K, V]) Set(key K, value V) {
	c.client.SetWithTTL(key, value, 1, c.ttl+rand.N(c.jitter+1))
}

func (c *Keyed[K, V]) SetFor(key K, value V, ttl time.Duration) {
	jitter := ttl / 10
	c.client.SetWithTTL(key, value, 1, ttl+rand.N(jitter+1))
}

func (c *Keyed[K, V]) Invalidate(key K) {
	c.group.Forget(c.keyFn(key))
	c.client.Delete(key)
}

func (c *Keyed[K, V]) Close() {
	c.client.Close()
}
