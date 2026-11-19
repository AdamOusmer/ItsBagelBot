// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type cacheGenerationKey struct{}

type cacheStripe struct {
	sync.Mutex
	generation atomic.Uint64
}

func (c *Cache) stripe(key string) *cacheStripe {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return &c.stripes[h.Sum64()%uint64(len(c.stripes))]
}

type cacheGeneration struct {
	cache *Cache
	key   string
	value uint64
}

func (c *Cache) guardFill(ctx context.Context, key string) context.Context {
	return c.fillContext(ctx, key)
}

func (c *Cache) fillContext(ctx context.Context, key string) context.Context {
	fill, _ := ctx.Value(cacheGenerationKey{}).(cacheGeneration)
	if fill.matches(c, key) {
		return ctx
	}
	return context.WithValue(ctx, cacheGenerationKey{}, cacheGeneration{cache: c, key: key, value: c.stripe(key).generation.Load()})
}

func detachedFill(parent context.Context) context.Context {
	return context.WithValue(DetachedRequestTelemetry(parent), cacheGenerationKey{}, parent.Value(cacheGenerationKey{}))
}

func flightKey(ctx context.Context) string {
	fill := ctx.Value(cacheGenerationKey{}).(cacheGeneration)
	return fill.key + ":" + strconv.FormatUint(fill.value, 10)
}

func (c *Cache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	stripe := c.stripe(key)
	stripe.Lock()
	defer stripe.Unlock()
	fill, _ := ctx.Value(cacheGenerationKey{}).(cacheGeneration)
	if fill.matches(c, key) && fill.value != stripe.generation.Load() {
		return nil
	}
	return c.store.Set(ctx, key, value, ttl)
}

func (g cacheGeneration) matches(c *Cache, key string) bool {
	return g.cache == c && g.key == key
}
