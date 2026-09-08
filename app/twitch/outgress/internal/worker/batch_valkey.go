// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const batchKeyPrefix = "outgress:batch:"

// ValkeyBatchStore is the infrastructure adapter for BatchStore. Batch
// orchestration depends only on the interface in batch.go; this file owns the
// Valkey commands and key layout.
type ValkeyBatchStore struct {
	client valkey.Client
}

// NewValkeyBatchStore builds the store on a primary-consistent view. The lock
// holder reads back its own progress: Acquire takes the lock, then Next reads
// the cursor SaveNext wrote on the previous pass. A node-local replica that has
// not yet received that SaveNext returns an older cursor, and the batch resends
// chat lines it already delivered. The view reuses the client's connections.
func NewValkeyBatchStore(client valkey.Client) *ValkeyBatchStore {
	return &ValkeyBatchStore{client: pkg_valkey.Primary(client)}
}

func (s *ValkeyBatchStore) Acquire(ctx context.Context, lease BatchLease, ttl time.Duration) (bool, error) {
	return s.lock(lease).Acquire(ctx, ttl)
}

func (s *ValkeyBatchStore) Next(ctx context.Context, batchID string) (int, error) {
	value, err := s.client.Do(ctx, s.client.B().Get().Key(batchProgressKey(batchID)).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, err
	}
	return strconv.Atoi(value)
}

func (s *ValkeyBatchStore) SaveNext(ctx context.Context, batchID string, next int, ttl time.Duration) error {
	return s.client.Do(ctx, s.client.B().Set().Key(batchProgressKey(batchID)).
		Value(strconv.Itoa(next)).Px(ttl).Build()).Error()
}

func (s *ValkeyBatchStore) Release(ctx context.Context, lease BatchLease) error {
	return s.lock(lease).Release(ctx)
}

// lock is the batch's slice of the shared owner-scoped lock primitive: the
// lease names both the key and the owner token, so Acquire and Release derive
// the same lock from the same lease rather than each spelling the key out.
func (s *ValkeyBatchStore) lock(lease BatchLease) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, batchLockKey(lease.ID), lease.Owner)
}

func batchLockKey(batchID string) string     { return batchKeyPrefix + batchID + ":lock" }
func batchProgressKey(batchID string) string { return batchKeyPrefix + batchID + ":next" }
