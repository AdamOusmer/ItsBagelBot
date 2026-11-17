// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"errors"
	"time"

	pkgvalkey "ItsBagelBot/pkg/valkey"
)

type LeaseBatch struct {
	Keys   []EffectKey
	Owner  string
	Owners []string
	TTL    time.Duration
	Mode   string
}

type LeaseStore interface {
	UpdateLeases(context.Context, LeaseBatch) ([]int64, error)
}

func (s *ValkeyStore) UpdateLeases(ctx context.Context, batch LeaseBatch) ([]int64, error) {
	keys := make([]string, len(batch.Keys))
	for i, key := range batch.Keys {
		keys[i] = Key(s.prefix, key)
	}
	return pkgvalkey.UpdateEffectLeases(ctx, s.client, pkgvalkey.EffectLeases{
		Keys: keys, Owner: batch.Owner, Owners: batch.Owners, TTL: batch.TTL, Mode: batch.Mode,
	})
}

func (l *lruStore) UpdateLeases(ctx context.Context, batch LeaseBatch) ([]int64, error) {
	return UpdateLeases(ctx, l.inner, batch)
}

func UpdateLeases(ctx context.Context, store Store, batch LeaseBatch) ([]int64, error) {
	if leases, ok := store.(LeaseStore); ok {
		return leases.UpdateLeases(ctx, batch)
	}
	return nil, errors.New("idempotency store does not support owner leases")
}
