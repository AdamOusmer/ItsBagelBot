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
		if stale {
			c.refreshBytes(key, admit, build)
		}
		return payload, nil
	}

	if err := spend(ctx, admit); err != nil {
		return nil, err
	}

	res, err, _ := c.sf.Do(key, func() (any, error) {
		if b, ok, gerr := c.store.Get(ctx, key); gerr == nil && ok {
			if _, payload, valid := unwrapEntry(b); valid {
				return payload, nil
			}
		}

		payload, ttl, berr := build(ctx)
		if berr != nil {
			return nil, berr
		}
		c.storeEntry(ctx, key, payload, ttl)
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

func CachedBytesFresh(ctx context.Context, c *Cache, key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) ([]byte, error) {
	if err := spend(ctx, admit); err != nil {
		return nil, err
	}
	res, err, _ := c.sf.Do(key, func() (any, error) {
		payload, ttl, berr := build(ctx)
		if berr != nil {
			return nil, berr
		}
		c.storeEntry(ctx, key, payload, ttl)
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

func (c *Cache) readBytes(ctx context.Context, key string) (payload []byte, stale, ok bool) {
	b, found, err := c.store.Get(ctx, key)
	if err != nil || !found {
		return nil, false, false
	}
	fresh, payload, valid := unwrapEntry(b)
	if !valid {
		_ = c.store.Del(ctx, key)
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

func (c *Cache) refreshBytes(key string, admit func(context.Context) error, build func(context.Context) ([]byte, time.Duration, error)) {
	if _, busy := c.refreshing.LoadOrStore(key, struct{}{}); busy {
		return
	}
	go func() {
		defer c.refreshing.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), swrRefreshTimeout)
		defer cancel()
		if won, err := c.store.SetNX(ctx, key+":swr", swrRefreshTimeout); err != nil || !won {
			return
		}
		if err := spend(ctx, admit); err != nil {
			return
		}
		_, _, _ = c.sf.Do(key, func() (any, error) {
			payload, ttl, err := build(ctx)
			if err != nil {
				return nil, err
			}
			c.storeEntry(ctx, key, payload, ttl)
			return payload, nil
		})
	}()
}

func MarshalReply(v any) ([]byte, error) { return codec.Marshal(v) }
