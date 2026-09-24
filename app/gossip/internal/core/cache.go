// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/singleflight"
)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// val may be a pooled buffer recycled once Set returns; implementations must not retain it.
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	SetNX(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

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

func (s *ValkeyStore) SetNX(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return pkg_valkey.ClaimOnce(ctx, s.c, key, ttl)
}

func (s *ValkeyStore) Del(ctx context.Context, key string) error {
	return s.c.Do(ctx, s.c.B().Del().Key(key).Build()).Error()
}

type Cache struct {
	store      Store
	sf         singleflight.Group
	refreshing sync.Map
}

func NewCache(store Store) *Cache { return &Cache{store: store} }

func Key(provider, endpoint, id string) string {
	return "gossip:" + provider + ":" + endpoint + ":" + id
}

func CacheID(parts ...string) string {
	folded := make([]string, len(parts))
	for i, part := range parts {
		folded[i] = strings.ToLower(strings.TrimSpace(part))
	}
	return strings.Join(folded, ":")
}

type cacheEnvelope[T any] struct {
	Value T              `json:"v"`
	Error *UpstreamError `json:"e,omitempty"`
	Fresh int64          `json:"f,omitempty"`
}

type envelope[T any] struct {
	value    T
	negative *UpstreamError
	fresh    int64
}

func (e envelope[T]) stale() bool { return time.Now().UnixMilli() >= e.fresh }

func decodeEnvelope[T any](b []byte) (out envelope[T], ok bool) {
	var probe struct {
		Value codec.RawMessage `json:"v"`
		Error *UpstreamError   `json:"e"`
		Fresh int64            `json:"f"`
	}
	if err := codec.Unmarshal(b, &probe); err != nil {
		return out, false
	}
	out.fresh = probe.Fresh
	if probe.Error != nil && probe.Error.Status != 0 {
		out.negative = probe.Error
		return out, true
	}
	if len(probe.Value) == 0 {
		return out, false
	}
	if err := codec.Unmarshal(probe.Value, &out.value); err != nil {
		return envelope[T]{}, false
	}
	return out, true
}

func Cached[T any](ctx context.Context, c *Cache, key string, ttl, negativeTTL time.Duration, admit func(context.Context) error, fetch func(context.Context) (T, error)) (T, error) {
	f := envelopeFlight[T]{cache: c, key: key, ttl: ttl, negativeTTL: negativeTTL, admit: admit, fetch: fetch}
	var zero T

	if entry, ok := f.read(ctx); ok {
		if entry.negative == nil && entry.stale() {
			f.refresh()
		}
		return entry.answer()
	}

	if err := spend(ctx, admit); err != nil {
		return zero, err
	}

	res, err, _ := c.sf.Do(key, func() (any, error) { return f.fill(ctx) })
	if err != nil {
		return zero, err
	}
	if v, ok := res.(T); ok {
		return v, nil
	}
	return zero, fmt.Errorf("cache %s: unexpected value type %T", key, res)
}

type envelopeFlight[T any] struct {
	cache       *Cache
	key         string
	ttl         time.Duration
	negativeTTL time.Duration
	admit       func(context.Context) error
	fetch       func(context.Context) (T, error)
}

func (f envelopeFlight[T]) read(ctx context.Context) (envelope[T], bool) {
	b, found, err := f.cache.store.Get(ctx, f.key)
	if err != nil || !found {
		return envelope[T]{}, false
	}
	entry, ok := decodeEnvelope[T](b)
	if !ok {
		_ = f.cache.store.Del(ctx, f.key)
		return envelope[T]{}, false
	}
	return entry, true
}

func (e envelope[T]) answer() (T, error) {
	if e.negative != nil {
		var zero T
		return zero, e.negative
	}
	return e.value, nil
}

func (f envelopeFlight[T]) fill(ctx context.Context) (any, error) {
	if entry, ok := f.read(ctx); ok {
		return entry.answer()
	}
	return f.refetch(ctx)
}

func (f envelopeFlight[T]) refetch(ctx context.Context) (any, error) {
	v, ferr := f.fetch(ctx)
	env, cacheTTL, err := f.envelopeFor(v, ferr)
	if err != nil {
		return nil, err
	}
	if b, merr := codec.Marshal(env); merr == nil {
		_ = f.cache.store.Set(ctx, f.key, b, cacheTTL)
	}
	if env.Error != nil {
		return v, env.Error
	}
	return v, nil
}

func (f envelopeFlight[T]) envelopeFor(v T, ferr error) (cacheEnvelope[T], time.Duration, error) {
	if ferr == nil {
		return cacheEnvelope[T]{Value: v, Fresh: time.Now().Add(f.ttl).UnixMilli()}, 2 * f.ttl, nil
	}
	var ue *UpstreamError
	if errors.As(ferr, &ue) && (ue.Status == 404 || ue.Status == 400) {
		return cacheEnvelope[T]{Error: ue}, f.negativeTTL, nil
	}
	return cacheEnvelope[T]{}, 0, ferr
}

func (f envelopeFlight[T]) refresh() {
	if _, busy := f.cache.refreshing.LoadOrStore(f.key, struct{}{}); busy {
		return
	}
	go func() {
		defer f.cache.refreshing.Delete(f.key)
		ctx, cancel := context.WithTimeout(context.Background(), swrRefreshTimeout)
		defer cancel()
		if won, err := f.cache.store.SetNX(ctx, f.key+":swr", swrRefreshTimeout); err != nil || !won {
			return
		}
		if err := spend(ctx, f.admit); err != nil {
			return
		}
		_, _, _ = f.cache.sf.Do(f.key, func() (any, error) { return f.refetch(ctx) })
	}()
}

type StoreRequest[T any] struct {
	Key         string
	TTL         time.Duration
	NegativeTTL time.Duration
	Value       T
	Err         error
}

func StoreCached[T any](ctx context.Context, c *Cache, req StoreRequest[T]) {
	f := envelopeFlight[T]{cache: c, key: req.Key, ttl: req.TTL, negativeTTL: req.NegativeTTL}
	env, storeTTL, err := f.envelopeFor(req.Value, req.Err)
	if err != nil {
		return
	}
	if b, merr := codec.Marshal(env); merr == nil {
		_ = c.store.Set(ctx, req.Key, b, storeTTL)
	}
}

func (c *Cache) GetJSON(ctx context.Context, key string, out any) (bool, error) {
	b, ok, err := c.store.Get(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := codec.Unmarshal(b, out); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, key, b, ttl)
}

func (c *Cache) DelJSON(ctx context.Context, key string) error {
	return c.store.Del(ctx, key)
}

func (c *Cache) Exists(ctx context.Context, key string) (bool, error) {
	_, found, err := c.store.Get(ctx, key)
	return found, err
}

func (c *Cache) Claim(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.store.SetNX(ctx, key, ttl)
}
