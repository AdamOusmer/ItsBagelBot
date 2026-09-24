// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cache

import "time"

const DefaultCapacity int64 = 10000

type Cache[V any] = Keyed[string, V]

func identityKey(key string) string { return key }

func New[V any](capacity int64, ttl time.Duration) *Cache[V] {
	return NewKeyed[string, V](capacity, ttl, identityKey)
}
