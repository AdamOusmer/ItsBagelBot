// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const freezeChannel = 2
const freezeViewer = 7

func setFreeze(t *testing.T, r *loyaltyrepo.Loyalty) loyaltyrepo.FreezeState {
	t.Helper()
	state, err := r.Freeze(t.Context(), loyaltyrepo.FreezeRequest{UserID: freezeChannel, Minutes: 10})
	require.NoError(t, err)
	return state
}

func takeSnapshot(t *testing.T, r *loyaltyrepo.Loyalty) loyaltyrepo.FreezeState {
	t.Helper()
	req := loyaltyrepo.SnapshotRequest{UserID: freezeChannel, Owner: "round:abc", ExpiresAt: time.Now().Add(time.Hour)}
	state, err := r.CreateSnapshot(t.Context(), req)
	require.NoError(t, err)
	return state
}

func TestFreezeSnapshotKeepsStandingsWhileBalancesChange(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	ctx := t.Context()
	award := watchAward(freezeChannel, freezeViewer, 100)
	require.NoError(t, r.ApplyWatchAward(ctx, award))
	state := takeSnapshot(t, r)
	award.WindowID = "next"
	require.NoError(t, r.ApplyWatchAward(ctx, award))
	_, _, err := r.BalanceAdjustViewer(ctx, loyaltyrepo.BalanceAdjustment{UserID: freezeChannel, ViewerID: freezeViewer, ViewerLogin: "viewer", Value: 5})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		frozen bool
		points int64
		watch  uint64
	}{
		{"frozen", true, 10, 300},
		{"live", false, 25, 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, found, err := r.BalanceGet(ctx, freezeChannel, freezeViewer)
			if tc.frozen {
				row, found, err = r.SnapshotBalance(ctx, freezeChannel, freezeViewer, state.SnapshotID)
			}
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, []any{tc.points, tc.watch}, []any{row.Points, row.WatchSeconds})
		})
	}
}

func ageFreeze(t *testing.T, raw *sql.DB, state loyaltyrepo.FreezeState) {
	t.Helper()
	end := time.Now().Add(-3 * time.Minute).UnixMilli()
	for _, table := range []string{"loyalty_freezes", "loyalty_snapshots"} {
		_, err := raw.ExecContext(t.Context(), "UPDATE "+table+" SET expires_at_ms=? WHERE user_id=? AND snapshot_id=?", end, freezeChannel, state.SnapshotID)
		require.NoError(t, err)
	}
}

func TestFreezeExpiryAndReset(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.ApplyWatchAward(ctx, watchAward(freezeChannel, freezeViewer, 100)))
	state := setFreeze(t, r)

	for _, tc := range []struct {
		name    string
		expired bool
		count   int64
		err     error
	}{
		{"active", false, 0, loyaltyrepo.ErrFrozen},
		{"expired", true, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expired {
				ageFreeze(t, raw, state)
			}
			status, err := r.FreezeStatus(ctx, freezeChannel)
			require.NoError(t, err)
			assert.Equal(t, !tc.expired, status.Active(time.Now()))
			count, err := r.BalanceReset(ctx, freezeChannel)
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.count, count)
		})
	}
}

func TestAccountCleanupRemovesFreeze(t *testing.T) {
	for _, method := range []string{"delete", "recreate"} {
		t.Run(method, func(t *testing.T) {
			r, raw := sqliteRetentionRepo(t)
			ctx := t.Context()
			require.NoError(t, r.ApplyWatchAward(ctx, watchAward(freezeChannel, freezeViewer, 100)))
			setFreeze(t, r)
			takeSnapshot(t, r)
			switch method {
			case "delete":
				require.NoError(t, r.DeleteAccount(ctx, freezeChannel, 100))
			case "recreate":
				require.NoError(t, r.RestoreUser(ctx, freezeChannel, 101))
			}
			for _, table := range []string{"loyalty_freezes", "loyalty_snapshots", "loyalty_freeze_balances"} {
				var n int
				require.NoError(t, raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE user_id=?", freezeChannel).Scan(&n))
				assert.Zero(t, n, table)
			}
		})
	}
}

func TestPruneFreezeSnapshotsKeepsActiveEpoch(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	_, err := raw.ExecContext(ctx, `WITH RECURSIVE ids(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM ids WHERE i<1001)
		INSERT INTO balances (user_id,viewer_id,points,watch_seconds,created_at,updated_at) SELECT ?,i,100,0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM ids`, freezeChannel)
	require.NoError(t, err)
	old := setFreeze(t, r)
	ageFreeze(t, raw, old)
	active := setFreeze(t, r)
	require.NoError(t, r.PruneFreezeSnapshots(ctx))

	for _, tc := range []struct {
		id    loyaltyrepo.SnapshotID
		count int
		err   error
	}{{old.SnapshotID, 0, loyaltyrepo.ErrStaleFreezePin}, {active.SnapshotID, 1001, nil}} {
		_, err := r.FreezeEpoch(ctx, freezeChannel, tc.id)
		require.ErrorIs(t, err, tc.err)
		var count int
		require.NoError(t, raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM loyalty_freeze_balances WHERE user_id=? AND snapshot_id=?", freezeChannel, tc.id).Scan(&count))
		assert.Equal(t, tc.count, count)
	}
}

func TestFrozenTopPage(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	ctx := t.Context()
	for _, row := range []loyaltyrepo.BalanceAdjustment{
		{UserID: freezeChannel, ViewerID: 9, ViewerLogin: "nine", Value: 20},
		{UserID: freezeChannel, ViewerID: 7, ViewerLogin: "seven", Value: 20},
		{UserID: freezeChannel, ViewerID: 5, ViewerLogin: "five", Value: 25},
	} {
		_, found, err := r.BalanceAdjustViewer(ctx, row)
		require.NoError(t, err)
		require.True(t, found)
	}
	state := setFreeze(t, r)
	for _, tc := range []struct {
		name  string
		query loyaltyrepo.TopQuery
		total int64
		rows  []frozenRank
	}{
		{"podium", loyaltyrepo.TopQuery{Limit: 3}, 0, []frozenRank{{5, 1, "five"}, {7, 2, "seven"}, {9, 3, "nine"}}},
		{"page", loyaltyrepo.TopQuery{Offset: 1, Limit: 1}, 3, []frozenRank{{7, 2, "seven"}}},
		{"id ignores login hint", loyaltyrepo.TopQuery{ViewerID: 9, ViewerLogin: "renamed"}, 1, []frozenRank{{9, 3, "nine"}}},
		{"search login", loyaltyrepo.TopQuery{ViewerLogin: "seven"}, 1, []frozenRank{{7, 2, "seven"}}},
		{"unknown login", loyaltyrepo.TopQuery{ViewerLogin: "renamed"}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := r.FrozenTopPage(ctx, freezeChannel, state.SnapshotID, tc.query)
			require.NoError(t, err)
			var got []frozenRank
			for _, row := range page.Rows {
				got = append(got, frozenRank{row.ViewerID, row.Rank, row.ViewerLogin})
			}
			assert.Equal(t, tc.rows, got)
			assert.Equal(t, tc.total, page.Total)
		})
	}
}

type frozenRank struct {
	viewer uint64
	rank   int64
	login  string
}
