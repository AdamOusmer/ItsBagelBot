// Package core holds the gateway's provider-neutral runtime pieces: the
// Valkey-backed reply cache and the outbound HTTP fetcher. Providers compose
// these; core knows nothing about any specific external API.
package core

import (
	"context"
	"encoding/json"
	"errors"
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

// cacheEnvelope wraps a cached item so we can store both successful values and
// intentional negative responses (like 404 Not Found) without needing the caller
// to know how to serialize bare errors.
type cacheEnvelope[T any] struct {
	Value T              `json:"v,omitempty"`
	Error *UpstreamError `json:"e,omitempty"`
}

// Cached returns the cached T under key, or runs fetch to fill it for ttl.
// Only successful fetches are cached, EXCEPT for typed *UpstreamError failures
// with status 400 or 404 (e.g. "player not found"), which are negatively cached
// for negativeTTL to prevent repeated lookups of missing resources. A Store
// read/write error degrades to a direct fetch rather than failing the lookup.
func Cached[T any](ctx context.Context, c *Cache, key string, ttl, negativeTTL time.Duration, fetch func(context.Context) (T, error)) (T, error) {
	var zero T
	if b, ok, err := c.store.Get(ctx, key); err == nil && ok {
		var env cacheEnvelope[T]
		if err := json.Unmarshal(b, &env); err == nil {
			if env.Error != nil {
				return zero, env.Error
			}
			return env.Value, nil
		}
		// Poison entry (shape drift after a deploy): drop it and refetch.
		_ = c.store.Del(ctx, key)
	}

	res, err, _ := c.sf.Do(key, func() (any, error) {
		// A previous flight may have filled the key while we queued.
		if b, ok, gerr := c.store.Get(ctx, key); gerr == nil && ok {
			var env cacheEnvelope[T]
			if uerr := json.Unmarshal(b, &env); uerr == nil {
				if env.Error != nil {
					return zero, env.Error
				}
				return env.Value, nil
			}
		}
		v, ferr := fetch(ctx)
		var ue *UpstreamError
		var env cacheEnvelope[T]
		var cacheTTL time.Duration

		if ferr != nil {
			if errors.As(ferr, &ue) && (ue.Status == 404 || ue.Status == 400) {
				// Negative cache hit
				env.Error = ue
				cacheTTL = negativeTTL
			} else {
				return nil, ferr
			}
		} else {
			env.Value = v
			cacheTTL = ttl
		}

		if b, merr := json.Marshal(env); merr == nil {
			_ = c.store.Set(ctx, key, b, cacheTTL)
		}
		
		if env.Error != nil {
			return v, env.Error
		}
		return v, nil
	})
	if err != nil {
		return zero, err
	}
	if v, ok := res.(T); ok {
		return v, nil
	}
	return zero, fmt.Errorf("cache %s: unexpected value type %T", key, res)
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
