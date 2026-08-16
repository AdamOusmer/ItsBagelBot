// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package core holds gossip's provider-neutral runtime pieces: the
// Valkey-backed reply cache and the outbound HTTP fetcher. Providers compose
// these; core knows nothing about any specific external API.
package core

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/singleflight"
)

// Store is the byte-level cache surface Cache runs on. Valkey in production;
// tests substitute an in-memory map.
type Store interface {
	// Get returns the cached bytes and whether the key existed.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Set writes val under key for ttl. val may come from a pooled buffer the
	// caller recycles as soon as Set returns, so an implementation must not
	// retain it (the valkey client serializes within the call; an in-memory
	// test store must copy).
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// SetNX writes val under key for ttl only when the key is absent and
	// reports whether this caller won the claim. It is the fleet-wide mutual
	// exclusion primitive: coordination lives in the shared store, never in
	// pod-local state, so replicas cannot each make the same decision.
	SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error)
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

// SetNX claims key with SET NX EX. A lost claim answers as a Valkey nil reply,
// which is a normal outcome, not an error. Writes route to the Sentinel-elected
// master, so the claim is authoritative fleet-wide.
func (s *ValkeyStore) SetNX(ctx context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	res := s.c.Do(ctx, s.c.B().Set().Key(key).Value(valkey.BinaryString(val)).Nx().Ex(ttl).Build())
	if err := res.Error(); err != nil {
		if valkey.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *ValkeyStore) Del(ctx context.Context, key string) error {
	return s.c.Do(ctx, s.c.B().Del().Key(key).Build()).Error()
}

// Cache is the shared reply cache every provider endpoint reads through. It
// stores marshaled JSON replies in the Store under
// "gossip:<provider>:<endpoint>:<key>" and collapses concurrent misses for
// the same key through singleflight, so a chat spike on one player costs one
// upstream call.
type Cache struct {
	store Store
	sf    singleflight.Group
	// refreshing dedups background SWR revalidations: one per key at a time
	// (see refreshBytes). Keys are cleared as each refresh finishes.
	refreshing sync.Map
}

func NewCache(store Store) *Cache { return &Cache{store: store} }

// Key builds the canonical cache key for one endpoint lookup.
func Key(provider, endpoint, id string) string {
	return "gossip:" + provider + ":" + endpoint + ":" + id
}

// cacheEnvelope wraps a cached item so we can store both successful values and
// intentional negative responses (like 404 Not Found) without needing the caller
// to know how to serialize bare errors. Value carries no omitempty: the "v" member
// doubles as the format marker decodeEnvelope requires, so a success entry always
// has one even when the value is a zero string.
type cacheEnvelope[T any] struct {
	Value T              `json:"v"`
	Error *UpstreamError `json:"e,omitempty"`
	// Fresh is the unix-millisecond instant this entry stops being fresh. The
	// record is retained for twice its fresh window, so the span between the two
	// is the stale tail Cached serves from while it revalidates behind the
	// caller. An entry written before this field existed decodes it as zero,
	// which reads as "already stale": that entry is served once and refreshed,
	// so the format rolls forward on its own rather than needing a flush.
	Fresh int64 `json:"f,omitempty"`
}

// envelope is one decoded entry: the value, the negative it stands in for, and
// when it stops being fresh.
type envelope[T any] struct {
	value    T
	negative *UpstreamError
	fresh    int64
}

// stale reports whether the entry has passed its fresh window and should be
// revalidated behind the caller it is about to be served to.
func (e envelope[T]) stale() bool { return time.Now().UnixMilli() >= e.fresh }

// decodeEnvelope reads one cached entry. ok is false for anything that is not a
// well-formed envelope — including a legacy/foreign-format entry that happens to
// be valid JSON. Requiring the "v"/"e" marker matters: unmarshaling an old-format
// reply into the envelope would silently succeed with a zero Value, and the caller
// would serve an empty reply (blank player, all-zero stats) until the entry
// expired. That exact bug shipped once; the marker check is its regression guard.
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
		return out, false // no marker: legacy or foreign format
	}
	if err := codec.Unmarshal(probe.Value, &out.value); err != nil {
		return envelope[T]{}, false
	}
	return out, true
}

// Cached returns the cached T under key, or runs fetch to fill it for ttl.
// Only successful fetches are cached, EXCEPT for typed *UpstreamError failures
// with status 400 or 404 (e.g. "player not found"), which are negatively cached
// for negativeTTL to prevent repeated lookups of missing resources. A Store
// read/write error degrades to a direct fetch rather than failing the lookup.
//
// admit runs ONCE PER CALLER that reaches the miss path, before that caller joins
// the flight, and its error is that caller's alone; fetch runs once for the whole
// flight. This is the same split CachedBytes makes, for the same reason: a flight
// answers "who pays for the upstream call" and admission answers "may THIS request
// spend budget", and writing the budget check inside fetch collapsed the two, so
// whichever caller won the flight decided for everyone joined to it. The concrete
// failure was the premium reserve — a standard caller with a drained bucket handed
// its denial to premium callers entitled to the 25% reserve. Admission runs after
// the hit check because a hit costs no upstream call and so must cost no budget.
// A nil admit means the lookup spends none.
// A fresh hit returns as-is. A hit past its fresh window returns the stored value
// IMMEDIATELY and revalidates behind the caller, so the slow upstream leaves the
// critical path after the first cold fetch. That matters most on the dependent
// leg of a lookup: a fortnite display-name resolve runs BEFORE the stats call and
// the stats call needs its answer, so without this an expired account entry put a
// whole upstream round trip in front of a command that was otherwise warm.
//
// A negative is never revalidated in the background. It stands for an absence, so
// refreshing it would spend upstream budget to re-learn nothing; it simply expires.
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

// envelopeFlight is one Cached lookup's parameters travelling together. They are
// bundled because every step below needs most of them, and threading six
// arguments through each would be its own kind of unreadable.
type envelopeFlight[T any] struct {
	cache       *Cache
	key         string
	ttl         time.Duration
	negativeTTL time.Duration
	admit       func(context.Context) error
	fetch       func(context.Context) (T, error)
}

// read returns the stored entry, dropping one whose format does not parse so the
// caller's refetch replaces it rather than serving a zero value out of a legacy
// or foreign record.
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

// answer turns one decoded entry into the pair its caller receives.
func (e envelope[T]) answer() (T, error) {
	if e.negative != nil {
		var zero T
		return zero, e.negative
	}
	return e.value, nil
}

// fill is the foreground flight body: another flight may have filled the key
// while this caller queued, so it re-reads before spending an upstream call.
func (f envelopeFlight[T]) fill(ctx context.Context) (any, error) {
	if entry, ok := f.read(ctx); ok {
		return entry.answer()
	}
	return f.refetch(ctx)
}

// refetch runs the fetch unconditionally and stores what it returns. The
// double-check read fill does is deliberately absent: a revalidation runs while
// the stale entry is still present, so re-reading it would hand back that stale
// value and skip the refresh this whole path exists to perform.
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

// envelopeFor shapes one fetch outcome into the record to store and how long to
// keep it. A success is retained for twice its fresh window, so the second half
// is the stale tail; a 400/404 is the absence worth remembering for negativeTTL;
// anything else is an infrastructure failure that must not be cached at all.
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

// refresh revalidates one stale key in the background under the same discipline
// refreshBytes uses: the pod-local map is a cheap pre-filter, the SET NX claim in
// the shared store is the authority, so a stale key costs ONE upstream call
// fleet-wide rather than one per replica. It spends budget after winning the
// claim, so only the replica that will actually call the upstream pays. A failure
// is swallowed and the stale entry keeps serving until its physical TTL.
func (f envelopeFlight[T]) refresh() {
	if _, busy := f.cache.refreshing.LoadOrStore(f.key, struct{}{}); busy {
		return
	}
	go func() {
		defer f.cache.refreshing.Delete(f.key)
		ctx, cancel := context.WithTimeout(context.Background(), swrRefreshTimeout)
		defer cancel()
		if won, err := f.cache.store.SetNX(ctx, f.key+":swr", []byte("1"), swrRefreshTimeout); err != nil || !won {
			return
		}
		if err := spend(ctx, f.admit); err != nil {
			return
		}
		_, _, _ = f.cache.sf.Do(f.key, func() (any, error) { return f.refetch(ctx) })
	}()
}

// GetJSON reads a raw (non-fetching) entry, for provider-owned state like the
// mcsr stream-start snapshot.
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

// SetJSON writes a raw entry for ttl.
func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error {
	b, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	return c.store.Set(ctx, key, b, ttl)
}
