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

type ValkeyBatchStore struct {
	client valkey.Client
}

func NewValkeyBatchStore(client valkey.Client) *ValkeyBatchStore {
	return &ValkeyBatchStore{client: pkg_valkey.Primary(client)}
}

func (s *ValkeyBatchStore) Acquire(ctx context.Context, lease BatchLease, ttl time.Duration) (bool, error) {
	return s.lock(lease).Acquire(ctx, ttl)
}

func (s *ValkeyBatchStore) Next(ctx context.Context, batchID string) (int, error) {
	n, ok, err := pkg_valkey.NewKV(s.client).ReadInt(ctx, batchProgressKey(batchID))
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return int(n), nil
}

func (s *ValkeyBatchStore) SaveNext(ctx context.Context, lease BatchLease, next int, ttl time.Duration) error {
	return pkg_valkey.NewKV(s.client).SetMillis(ctx, batchProgressKey(lease.ID), strconv.Itoa(next), ttl)
}

func (s *ValkeyBatchStore) Release(ctx context.Context, lease BatchLease) error {
	return s.lock(lease).Release(ctx)
}

func (s *ValkeyBatchStore) lock(lease BatchLease) pkg_valkey.OwnerLock {
	return pkg_valkey.NewOwnerLock(s.client, batchLockKey(lease.ID), lease.Owner)
}

func batchLockKey(batchID string) string     { return batchKeyPrefix + batchID + ":lock" }
func batchProgressKey(batchID string) string { return batchKeyPrefix + batchID + ":next" }
