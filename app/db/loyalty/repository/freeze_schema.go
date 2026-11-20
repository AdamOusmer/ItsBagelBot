// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import "context"

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
	for _, stmt := range freezeTables {
		if _, err := r.sqldb.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return r.ensureFreezeIndexes(ctx)
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
