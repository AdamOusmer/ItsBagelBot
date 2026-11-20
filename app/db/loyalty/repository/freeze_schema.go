// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import "context"

const freezeCopy = `INSERT INTO loyalty_freeze_balances (user_id, snapshot_id, viewer_id, points, watch_seconds, login, viewer_name)
	SELECT user_id, ?, viewer_id, points, watch_seconds, COALESCE(viewer_login, ''), COALESCE(viewer_name, '')
	FROM balances WHERE user_id=?`

var freezeTables = []string{
	`CREATE TABLE IF NOT EXISTS loyalty_freezes (
		user_id BIGINT PRIMARY KEY,
		snapshot_id VARCHAR(32) NOT NULL DEFAULT '',
		as_of_ms BIGINT NOT NULL DEFAULT 0,
		expires_at_ms BIGINT NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS loyalty_snapshots (
		user_id BIGINT NOT NULL, snapshot_id VARCHAR(32) NOT NULL, expires_at_ms BIGINT NOT NULL,
		as_of_ms BIGINT NOT NULL, owner VARBINARY(255) NULL,
		pruning INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, snapshot_id))`,
	`CREATE TABLE IF NOT EXISTS loyalty_freeze_balances (
		user_id BIGINT NOT NULL, snapshot_id VARCHAR(32) NOT NULL, viewer_id BIGINT NOT NULL,
		points BIGINT NOT NULL, watch_seconds BIGINT UNSIGNED NOT NULL,
		login VARCHAR(64) NOT NULL DEFAULT '', viewer_name VARCHAR(64) NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, snapshot_id, viewer_id))`,
}

func (r *Loyalty) ensureFreezeTables(ctx context.Context) error {
	if err := r.renameFreezeEpochs(ctx); err != nil {
		return err
	}
	for _, stmt := range freezeTables {
		if _, err := r.sqldb.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := r.migrateFreezeColumns(ctx); err != nil {
		return err
	}
	return r.ensureFreezeIndexes(ctx)
}

func (r *Loyalty) renameFreezeEpochs(ctx context.Context) error {
	old := watchSchemaColumn{table: watchHistoryTable{name: "loyalty_freeze_epochs"}, name: "freeze_id"}
	present, err := r.watchColumnPresent(ctx, old)
	if err != nil || !present {
		return err
	}
	_, err = r.sqldb.ExecContext(ctx, "ALTER TABLE loyalty_freeze_epochs RENAME TO loyalty_snapshots")
	return ignoreMySQLSchemaRace(err, 1146)
}

func (r *Loyalty) migrateFreezeColumns(ctx context.Context) error {
	public := watchHistoryTable{name: "loyalty_freezes"}
	snapshots := watchHistoryTable{name: "loyalty_snapshots"}
	for _, column := range []watchSchemaColumn{
		{table: public, name: "freeze_id", definition: "RENAME COLUMN freeze_id TO snapshot_id"},
		{table: snapshots, name: "freeze_id", definition: "RENAME COLUMN freeze_id TO snapshot_id"},
		{table: snapshots, name: "ends_at_ms", definition: "RENAME COLUMN ends_at_ms TO expires_at_ms"},
		{table: watchHistoryTable{name: frozenBalances}, name: "freeze_id", definition: "RENAME COLUMN freeze_id TO snapshot_id"},
		{table: public, name: "block_transfer", definition: "DROP COLUMN block_transfer"},
		{table: public, name: "close_games", definition: "DROP COLUMN close_games"},
	} {
		if err := r.alterLegacyFreezeColumn(ctx, column); err != nil {
			return err
		}
	}
	if err := r.ensureWatchColumn(ctx, watchSchemaColumn{table: snapshots, name: "owner", definition: "VARBINARY(255) NULL"}); err != nil {
		return err
	}
	return r.migratePublicPause(ctx, public)
}

func (r *Loyalty) alterLegacyFreezeColumn(ctx context.Context, column watchSchemaColumn) error {
	present, err := r.watchColumnPresent(ctx, column)
	if err != nil || !present {
		return err
	}
	_, err = r.sqldb.ExecContext(ctx, "ALTER TABLE "+column.table.name+" "+column.definition)
	return ignoreMySQLSchemaRace(ignoreMySQLSchemaRace(err, 1054), 1091)
}

func (r *Loyalty) migratePublicPause(ctx context.Context, table watchHistoryTable) error {
	column := watchSchemaColumn{table: table, name: "holders", definition: "DROP COLUMN holders"}
	present, err := r.watchColumnPresent(ctx, column)
	if err != nil || !present {
		return err
	}
	modEnd := "COALESCE(CAST(JSON_EXTRACT(holders,'$.mod') AS SIGNED),0)"
	_, err = r.sqldb.ExecContext(ctx, "UPDATE loyalty_freezes SET expires_at_ms="+modEnd+" WHERE expires_at_ms>"+modEnd)
	if err := ignoreMySQLSchemaRace(err, 1054); err != nil {
		return err
	}
	return r.alterLegacyFreezeColumn(ctx, column)
}

func (r *Loyalty) ensureFreezeIndexes(ctx context.Context) error {
	indexes := []struct {
		table watchHistoryTable
		index watchHistoryIndex
	}{
		{watchHistoryTable{name: frozenBalances}, watchHistoryIndex{suffix: "rank", columns: "user_id,snapshot_id,points DESC,viewer_id"}},
		{watchHistoryTable{name: frozenBalances}, watchHistoryIndex{suffix: "login", columns: "user_id,snapshot_id,login"}},
		{watchHistoryTable{name: "loyalty_snapshots"}, watchHistoryIndex{suffix: "ends", columns: "expires_at_ms"}},
		{watchHistoryTable{name: "loyalty_snapshots"}, watchHistoryIndex{suffix: "owner", columns: "user_id,owner", unique: true}},
	}
	for _, entry := range indexes {
		if err := r.ensureWatchIndex(ctx, entry.table, entry.index); err != nil {
			return err
		}
	}
	return nil
}
