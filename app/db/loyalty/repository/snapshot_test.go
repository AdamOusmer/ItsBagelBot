// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotsExpireAndPruneIndependently(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	award := watchAward(freezeChannel, freezeViewer, 100)
	require.NoError(t, r.ApplyWatchAward(ctx, award))
	private := takeSnapshot(t, r)
	public := setFreeze(t, r)
	_, err := raw.ExecContext(ctx, `UPDATE loyalty_snapshots SET expires_at_ms=? WHERE user_id=? AND snapshot_id=?`,
		time.Now().Add(-time.Second).UnixMilli(), freezeChannel, private.SnapshotID)
	require.NoError(t, err)
	_, _, err = r.SnapshotBalance(ctx, freezeChannel, freezeViewer, private.SnapshotID)
	require.ErrorIs(t, err, loyaltyrepo.ErrSnapshotGone)
	_, err = r.BalanceCountAtLeast(ctx, freezeChannel, private.SnapshotID, loyaltyrepo.BalanceMinimums{})
	require.ErrorIs(t, err, loyaltyrepo.ErrSnapshotGone)

	_, err = r.CreateSnapshot(ctx, loyaltyrepo.SnapshotRequest{UserID: freezeChannel, Owner: "round:abc", ExpiresAt: time.Now().Add(time.Hour)})
	require.ErrorIs(t, err, loyaltyrepo.ErrSnapshotGone)

	require.NoError(t, r.PruneFreezeSnapshots(ctx))
	for _, tc := range []struct {
		id   loyaltyrepo.SnapshotID
		rows int
	}{{private.SnapshotID, 0}, {public.SnapshotID, 1}} {
		var count int
		err := raw.QueryRowContext(ctx, `SELECT COUNT(*) FROM loyalty_freeze_balances WHERE user_id=? AND snapshot_id=?`, freezeChannel, tc.id).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, tc.rows, count)
	}
}
