// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/testdb"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func watchRetentionRepo(t *testing.T, raw *sql.DB, dialect string) *loyaltyrepo.Loyalty {
	t.Helper()
	drv := entsql.OpenDB(dialect, raw)
	client := ent.NewClient(ent.Driver(drv))
	require.NoError(t, client.Schema.Create(t.Context()))
	r := loyaltyrepo.NewLoyalty(client, drv, nil, zap.NewNop())
	require.NoError(t, r.EnsureWatchSchema(t.Context()))
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

func sqliteRetentionRepo(t *testing.T) (*loyaltyrepo.Loyalty, *sql.DB) {
	t.Helper()
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name("watchretention"+strconv.FormatInt(time.Now().UnixNano(), 10))))
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	return watchRetentionRepo(t, raw, "sqlite3"), raw
}

func TestWatchRetentionRejectsReplayAfterPruning(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	checkWatchRetentionReplay(t, r, raw)
}

func checkWatchRetentionReplay(t *testing.T, r *loyaltyrepo.Loyalty, raw *sql.DB) {
	t.Helper()
	ctx := t.Context()
	now := time.Now()
	a := watchAward(71, 81, 100)
	a.WindowStartedAtUnixMilli = now.Add(-9 * 24 * time.Hour).UnixMilli()
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		_, err := raw.ExecContext(ctx, "UPDATE "+table+" SET created_at=?", now.Add(-9*24*time.Hour).Unix())
		require.NoError(t, err)
	}
	cutoff := now.Add(-8 * 24 * time.Hour).UnixMilli()
	require.NoError(t, r.PruneWatchHistory(ctx, cutoff))
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		var n int
		require.NoError(t, raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n))
		require.Zero(t, n)
	}
	// Both a byte-for-byte replay and a new operation for the retired window
	// must be consumed without recreating deleted replay markers or credits.
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	a.Chunk++
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	row, found, err := r.BalanceGet(ctx, 71, 81)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	require.EqualValues(t, 300, row.WatchSeconds)
	// A lower future maintenance request cannot reopen retired windows.
	require.NoError(t, r.PruneWatchHistory(ctx, cutoff-1000))
	var finalized int64
	require.NoError(t, raw.QueryRowContext(ctx, "SELECT finalized_window_ms FROM loyalty_watch_retention WHERE id=1").Scan(&finalized))
	require.Equal(t, cutoff, finalized)
	a.WindowID = "current"
	a.WindowStartedAtUnixMilli = now.UnixMilli()
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	row, _, err = r.BalanceGet(ctx, 71, 81)
	require.NoError(t, err)
	require.EqualValues(t, 20, row.Points)
}

func TestWatchRetentionClampsAgeAndPreservesProtectedOutboxWindow(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	now := time.Now()
	protected := now.Add(-9 * 24 * time.Hour).UnixMilli()
	// The outbox floor is capped immediately below its oldest unpaid source.
	require.NoError(t, r.PruneWatchHistory(ctx, protected-1))
	a := watchAward(72, 82, 100)
	a.WindowStartedAtUnixMilli = protected
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	row, found, err := r.BalanceGet(ctx, 72, 82)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	// Even an unsafe caller's future time cannot shorten seven-day retention.
	require.NoError(t, r.PruneWatchHistory(ctx, now.UnixMilli()))
	var finalized int64
	require.NoError(t, raw.QueryRowContext(ctx, "SELECT finalized_window_ms FROM loyalty_watch_retention WHERE id=1").Scan(&finalized))
	require.LessOrEqual(t, finalized, time.Now().Add(-7*24*time.Hour).UnixMilli())
	require.GreaterOrEqual(t, finalized, now.Add(-7*24*time.Hour).UnixMilli())
}

func TestWatchRetentionDrainsMultipleBatches(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	old := time.Now().Add(-9 * 24 * time.Hour)
	tx, err := raw.BeginTx(ctx, nil)
	require.NoError(t, err)
	for i := 0; i < 1005; i++ {
		id := fmt.Sprintf("%064d", i)
		_, err := tx.ExecContext(ctx, "INSERT INTO loyalty_watch_operations (operation_id,payload_hash,user_id,created_at) VALUES (?, ?, 73, ?)", id, id, old.Unix())
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "INSERT INTO loyalty_watch_viewers (award_id,user_id,created_at) VALUES (?, 73, ?)", id, old.Unix())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	for _, expected := range []int{0, 0} {
		require.NoError(t, r.PruneWatchHistory(ctx, time.Now().Add(-8*24*time.Hour).UnixMilli()))
		for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
			var n int
			require.NoError(t, raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n))
			require.Equal(t, expected, n)
		}
	}
}

func TestWatchRetentionPreservesWindowAboveBarrierInSameCreatedSecond(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	cutoff := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	a := watchAward(75, 85, 100)
	a.WindowStartedAtUnixMilli = cutoff + 1
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	// SQL's creation clock can be in the same second or behind the source
	// clock. Creation-time pruning alone would remove a still-replayable row.
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		_, err := raw.ExecContext(ctx, "UPDATE "+table+" SET created_at=?", cutoff/1000)
		require.NoError(t, err)
	}
	require.NoError(t, r.PruneWatchHistory(ctx, cutoff))
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	a.Chunk++
	require.NoError(t, r.ApplyWatchAward(ctx, a))
	row, found, err := r.BalanceGet(ctx, 75, 85)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
}

func TestWatchRetentionMigratesExistingReplayTables(t *testing.T) {
	raw, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name("watchmigration"+strconv.FormatInt(time.Now().UnixNano(), 10))))
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	for _, stmt := range []string{
		`CREATE TABLE loyalty_watch_operations (operation_id VARCHAR(64) PRIMARY KEY, payload_hash VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL)`,
		`CREATE TABLE loyalty_watch_viewers (award_id VARCHAR(64) PRIMARY KEY, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL)`,
		`INSERT INTO loyalty_watch_operations VALUES ('old-operation','old-hash',76,1)`,
		`INSERT INTO loyalty_watch_viewers VALUES ('old-viewer',76,1)`,
	} {
		_, err := raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err)
	}
	r := watchRetentionRepo(t, raw, "sqlite3")
	require.NoError(t, r.EnsureWatchSchema(t.Context()), "schema setup is idempotent")
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		var stamp, n int64
		require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT window_started_at_ms FROM "+table).Scan(&stamp))
		require.Zero(t, stamp)
		require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", "idx_"+table+"_window").Scan(&n))
		require.EqualValues(t, 1, n)
	}
}
