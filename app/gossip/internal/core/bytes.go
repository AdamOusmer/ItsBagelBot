// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"time"
)

const swrRefreshTimeout = 15 * time.Second

func CachedBytes(ctx context.Context, c *Cache, key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if payload, stale, ok := c.readBytes(ctx, key); ok {
		outcome := "hit"
		if stale {
			outcome = "stale"
			c.refreshBytes(ctx, key, admit, build)
		}
		RecordCacheOutcome(ctx, outcome)
		return payload, nil
	}
	RecordCacheOutcome(ctx, "miss")

	if err := spend(ctx, admit); err != nil {
		RecordFetchError(ctx, err)
		return nil, err
	}

	res, err := c.load(ctx, key, func(ctx context.Context) (any, error) {
		if payload, _, ok := c.readBytes(ctx, key); ok {
			return payload, nil
		}

		payload, ttl, berr := build(ctx)
		if berr != nil {
			RecordFetchError(ctx, berr)
			return nil, berr
		}
		c.storeEntry(ctx, key, payload, ttl)
		return payload, nil
	})
	if err != nil {
		RecordFetchError(ctx, err)
		return nil, err
	}
	return res.([]byte), nil
}

func CachedBytesFresh(ctx context.Context, c *Cache, key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	RecordCacheOutcome(ctx, "miss")
	if err := spend(ctx, admit); err != nil {
		RecordFetchError(ctx, err)
		return nil, err
	}
	payload, ttl, err := build(ctx)
	if err != nil {
		RecordFetchError(ctx, err)
		return nil, err
	}
	c.storeEntry(ctx, key, payload, ttl)
	return payload, nil
}

func (c *Cache) readBytes(ctx context.Context, key string) (payload []byte, stale, ok bool) {
	b, found, err := c.store.Get(ctx, key)
	RecordCacheError(ctx, "read", err)
	if err != nil || !found {
		return nil, false, false
	}
	fresh, payload, valid := unwrapEntry(b)
	if !valid {
		RecordCacheError(ctx, "delete", c.store.Del(ctx, key))
		return nil, false, false
	}
	return payload, time.Now().UnixMilli() >= fresh, true
}

func spend(ctx context.Context, admit func(context.Context) error) error {
	if admit == nil {
		return nil
	}
	return admit(ctx)
}

func (c *Cache) refreshBytes(parent context.Context, key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) {
	if _, busy := c.refreshing.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer c.refreshing.Delete(key)
		ctx, cancel := WithFetchTimeout(DetachedRequestTelemetry(parent), swrRefreshTimeout)
		defer cancel()
		won, err := c.store.SetNX(ctx, key+":swr", swrRefreshTimeout)
		RecordCacheError(ctx, "claim", err)
		if err != nil || !won {
			return
		}
		if err := spend(ctx, admit); err != nil {
			RecordFetchError(ctx, err)
			return
		}
		RecordCacheOutcome(ctx, "refresh")
		_, err = c.load(ctx, key, func(ctx context.Context) (any, error) {
			payload, ttl, err := build(ctx)
			if err != nil {
				return nil, err
			}
			c.storeEntry(ctx, key, payload, ttl)
			return payload, nil
		})
		RecordCacheError(ctx, "refresh", err)
	}()
}

func MarshalReply(v any) ([]byte, error) { return codec.Marshal(v) }

// StoreCachedBytes seeds an ordinary byte-cache entry from an already fresh
// result. Callers must use the same payload shape and key as CachedBytes.
func StoreCachedBytes(ctx context.Context, c *Cache, entry BytesEntry) {
	c.storeEntry(ctx, entry.Key, entry.Value, entry.TTL)
}

type BytesEntry struct {
	Key   string
	Value []byte
	TTL   time.Duration
}
