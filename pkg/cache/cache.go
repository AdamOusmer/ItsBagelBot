// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import "time"

// DefaultCapacity is the fallback maximum number of entries for a cache, used
// when a caller has no reason to size the cache differently (tests, cold paths,
// development). It is deliberately generous so it is always safe; hot, always-on
// services should size their caches to their working set instead (see the
// per-cache capacity constants next to each cache's TTL) so a spam- or
// churn-driven fill cannot pin this many resident entries. theine allocates in
// proportion to live occupancy, so capacity is a ceiling on resident entries,
// not an up-front reservation.
const DefaultCapacity int64 = 10000

// Cache is an in-process TTL cache keyed by string: concurrent misses on the
// same key collapse into a single loader call, and expirations are jittered so
// entries written together do not all expire together.
//
// Adapter over Keyed with K = string. It used to be a second, byte-for-byte
// copy of Keyed's body; the copy drifted (Keyed grew an allocation-free hit
// path that Cache never got) which is exactly the failure an alias prevents.
// An alias rather than a wrapper struct so `*cache.Cache[V]` in the ~15 call
// sites keeps compiling and no method pays an extra indirection.
type Cache[V any] = Keyed[string, V]

// identityKey is the keyFn for a string-keyed cache: singleflight already keys
// on strings, so the key is its own flight id and no conversion is needed.
func identityKey(key string) string { return key }

// New creates a string-keyed cache with a maximum capacity whose entries live
// for ttl plus a random jitter in [0, ttl/10). Theine automatically evicts items
// when full or expired. Call Close when the cache is no longer needed.
func New[V any](capacity int64, ttl time.Duration) *Cache[V] {
	return NewKeyed[string, V](capacity, ttl, identityKey)
}
