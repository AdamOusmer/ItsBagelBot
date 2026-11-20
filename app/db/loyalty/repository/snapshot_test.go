// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/pkg/codec"

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

func TestLegacyStandingsMigrationPreservesCopiesAndModPause(t *testing.T) {
	for _, tc := range []struct {
		owner  loyaltyrepo.SnapshotOwner
		active bool
	}{{"mod", true}, {"round:legacy", false}} {
		t.Run(string(tc.owner), func(t *testing.T) {
			r, raw := sqliteRetentionRepo(t)
			checkLegacyStandingsMigration(t, migrationDB{repo: r, raw: raw}, tc.owner, tc.active)
		})
	}
}

func TestLegacyStandingsMigrationMySQL(t *testing.T) {
	r, raw := mysqlRetentionRepo(t)
	checkLegacyStandingsMigration(t, migrationDB{repo: r, raw: raw}, "mod", true)
}

type migrationDB struct {
	repo *loyaltyrepo.Loyalty
	raw  *sql.DB
}

func checkLegacyStandingsMigration(t *testing.T, db migrationDB, owner loyaltyrepo.SnapshotOwner, active bool) {
	t.Helper()
	legacy := legacyStandings{owner: owner, asOf: time.Now().Add(-time.Minute).UnixMilli()}
	legacy.seed(t, db.raw)
	for range 2 {
		require.NoError(t, db.repo.EnsureWatchSchema(t.Context()))
	}
	verifyMigratedStandings(t, db.repo, legacy, active)
}

type legacyStandings struct {
	owner string
	asOf  int64
}

func (l legacyStandings) seed(t *testing.T, raw *sql.DB) {
	t.Helper()
	ctx := t.Context()
	for _, stmt := range []string{
		`DROP TABLE loyalty_freezes`,
		`DROP TABLE loyalty_snapshots`,
		`DROP TABLE loyalty_freeze_balances`,
		`CREATE TABLE loyalty_freezes (
			user_id BIGINT PRIMARY KEY, freeze_id VARCHAR(32) NOT NULL DEFAULT '',
			as_of_ms BIGINT NOT NULL DEFAULT 0, expires_at_ms BIGINT NOT NULL DEFAULT 0,
			holders TEXT NOT NULL, block_transfer INTEGER NOT NULL DEFAULT 0, close_games INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE loyalty_freeze_epochs (
			user_id BIGINT NOT NULL, freeze_id VARCHAR(32) NOT NULL, ends_at_ms BIGINT NOT NULL,
			as_of_ms BIGINT NOT NULL, pruning INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (user_id, freeze_id))`,
		`CREATE TABLE loyalty_freeze_balances (
			user_id BIGINT NOT NULL, freeze_id VARCHAR(32) NOT NULL, viewer_id BIGINT NOT NULL,
			points BIGINT NOT NULL, watch_seconds BIGINT UNSIGNED NOT NULL,
			login VARCHAR(64) NOT NULL DEFAULT '', viewer_name VARCHAR(64) NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, freeze_id, viewer_id))`,
		`CREATE INDEX idx_loyalty_freeze_balances_rank ON loyalty_freeze_balances (user_id,freeze_id,points DESC,viewer_id)`,
	} {
		_, err := raw.ExecContext(ctx, stmt)
		require.NoError(t, err)
	}
	l.insert(t, raw)
}

func (l legacyStandings) insert(t *testing.T, raw *sql.DB) {
	t.Helper()
	ctx := t.Context()
	end := l.asOf + time.Hour.Milliseconds()
	holders, err := codec.Marshal(map[loyaltyrepo.SnapshotOwner]int64{l.owner: end})
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, `INSERT INTO loyalty_freezes (user_id,freeze_id,as_of_ms,expires_at_ms,holders,block_transfer,close_games)
		VALUES (2,'legacy',?,?,?,1,1)`, l.asOf, end, string(holders))
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, `INSERT INTO loyalty_freeze_epochs (user_id,freeze_id,as_of_ms,ends_at_ms) VALUES (2,'legacy',?,?)`, l.asOf, end)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, `INSERT INTO loyalty_freeze_balances (user_id,freeze_id,viewer_id,points,watch_seconds) VALUES (2,'legacy',7,100,200)`)
	require.NoError(t, err)
}

func verifyMigratedStandings(t *testing.T, r *loyaltyrepo.Loyalty, legacy legacyStandings, active bool) {
	t.Helper()
	ctx := t.Context()
	state, err := r.FreezeStatus(ctx, freezeChannel)
	require.NoError(t, err)
	assert.Equal(t, active, state.Active(time.Now()))

	pin, err := r.FreezeEpoch(ctx, freezeChannel, "legacy")
	require.NoError(t, err)
	assert.Equal(t, legacy.asOf, pin.AsOf.UnixMilli())
	page, err := r.FrozenTopPage(ctx, freezeChannel, pin.SnapshotID, loyaltyrepo.TopQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	assert.Equal(t, int64(100), page.Rows[0].Points)
}

func TestExtendPrivateSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name            string
		public, dropped bool
		want            bool
	}{
		{name: "extend private cutoff", want: true},
		{name: "public pause is independent", public: true},
		{name: "dropped cutoff stays expired", dropped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sqliteRetentionRepo(t)
			ctx := t.Context()
			state := takeSnapshot(t, r)
			if tc.public {
				state = setFreeze(t, r)
			}
			if tc.dropped {
				_, err := r.DropSnapshot(ctx, freezeChannel, state.SnapshotID)
				require.NoError(t, err)
			}
			req := loyaltyrepo.SnapshotExtension{UserID: freezeChannel, ID: state.SnapshotID, ExpiresAt: state.ExpiresAt.Add(time.Minute)}
			for range 2 {
				applied, err := r.ExtendSnapshot(ctx, req)
				require.NoError(t, err)
				require.Equal(t, tc.want, applied)
			}
		})
	}
}
