// Package core holds the gateway's provider-neutral runtime pieces: the
// Valkey-backed reply cache and the outbound HTTP fetcher. Providers compose
// these; core knows nothing about any specific external API.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/singleflight"
)

// Store is the byte-level cache surface Cache runs on. Valkey in production;
// tests substitute an in-memory map.
type Store interface {
	// Get returns the cached bytes and whether the key existed.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set writes val under key for ttl.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Del removes key.
	Del(ctx context.Context, key string) error
}

// ValkeyStore implements Store on the fleet's Valkey client (node-local reads,
// Sentinel-routed writes).
type ValkeyStore struct{ c valkey.Client }

func NewValkeyStore(c valkey.Client) *ValkeyStore { return &ValkeyStore{c: c} }

func (s *ValkeyStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	res := s.c.Do(ctx, s.c.B().Get().Key(key).Build())
	if err := res.Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	b, err := res.AsBytes()
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *ValkeyStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return s.c.Do(ctx, s.c.B().Set().Key(key).Value(valkey.BinaryString(val)).Ex(ttl).Build()).Error()
}

func (s *ValkeyStore) Del(ctx context.Context, key string) error {
	return s.c.Do(ctx, s.c.B().Del().Key(key).Build()).Error()
}

// Cache is the shared reply cache every provider endpoint reads through. It
// stores marshaled JSON replies in the Store under
// "gateway:<provider>:<endpoint>:<key>" and collapses concurrent misses for
// the same key through singleflight, so a chat spike on one player costs one
// upstream call.
type Cache struct {
	store Store
	sf    singleflight.Group
}

func NewCache(store Store) *Cache { return &Cache{store: store} }

// Key builds the canonical cache key for one endpoint lookup.
func Key(provider, endpoint, id string) string {
	return "gateway:" + provider + ":" + endpoint + ":" + id
}

// Cached returns the cached T under key, or runs fetch to fill it for ttl.
// Only successful fetches are cached: an upstream failure is returned to this
// caller and the next request retries. A Store read/write error degrades to a
// direct fetch rather than failing the lookup — the cache is an optimization,
// never a dependency.
func Cached[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, fetch func(context.Context) (T, error)) (T, error) {
	var zero T
	if b, ok, err := c.store.Get(ctx, key); err == nil && ok {
		var v T
		if err := json.Unmarshal(b, &v); err == nil {
			return v, nil
		}
		// Poison entry (shape drift after a deploy): drop it and refetch.
		_ = c.store.Del(ctx, key)
	}

	res, err, _ := c.sf.Do(key, func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if b, ok, gerr := c.store.Get(ctx, key); gerr == nil && ok {
			var v T
			if uerr := json.Unmarshal(b, &v); uerr == nil {
				return v, nil
			}
		}
		v, ferr := fetch(ctx)
		if ferr != nil {
			return nil, ferr
		}
		if b, merr := json.Marshal(v); merr == nil {
			_ = c.store.Set(ctx, key, b, ttl)
		}
		return v, nil
	})
	if err != nil {
		return zero, err
	}
	v, ok := res.(T)
	if !ok {
		return zero, fmt.Errorf("cache %s: unexpected value type %T", key, res)
	}
	return v, nil
}

// GetJSON reads a raw (non-fetching) entry, for provider-owned state like the
// mcsr stream-start snapshot.
func (c *Cache) GetJSON(ctx context.Context, key string, out any) (bool, error) {
	b, ok, err := c.store.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return false, err
	}
	return true, nil
}

// SetJSON writes a raw entry for ttl.
func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, key, b, ttl)
}
