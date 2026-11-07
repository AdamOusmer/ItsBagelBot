// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"

	livekey "ItsBagelBot/internal/domain/live"
	"ItsBagelBot/internal/domain/outgress"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

type RewardResetLookup interface {
	RestorePending(ctx context.Context, broadcasterID uint64, offlineVersion int64) (bool, error)
}

type ValkeyRewardResets struct {
	client valkey.Client
}

func NewValkeyRewardResets(client valkey.Client) *ValkeyRewardResets {
	return &ValkeyRewardResets{client: pkg_valkey.Primary(client)}
}

// An offline older than the applied live version belongs to a stream that already ended; the snapshot waits for the real one.
func (r *ValkeyRewardResets) RestorePending(ctx context.Context, broadcasterID uint64, offlineVersion int64) (bool, error) {
	keys := pkg_valkey.GuardedIntKeys{Guard: outgress.RewardSnapshotKey(strconv.FormatUint(broadcasterID, 10)), Value: livekey.VerKey(broadcasterID)}
	snapshot, err := keys.Read(ctx, r.client)
	if err != nil || !snapshot.Exists {
		return false, err
	}
	return !snapshot.HasValue || snapshot.Value <= offlineVersion, nil
}
