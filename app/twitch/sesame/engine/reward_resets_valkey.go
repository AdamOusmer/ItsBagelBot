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
	id := strconv.FormatUint(broadcasterID, 10)
	results := r.client.DoMulti(ctx,
		r.client.B().Exists().Key(outgress.RewardSnapshotKey(id)).Build(),
		r.client.B().Get().Key(livekey.VerKey(broadcasterID)).Build(),
	)
	snapshots, err := results[0].AsInt64()
	if err != nil || snapshots == 0 {
		return false, err
	}
	applied, err := results[1].AsInt64()
	if valkey.IsValkeyNil(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return applied <= offlineVersion, nil
}
