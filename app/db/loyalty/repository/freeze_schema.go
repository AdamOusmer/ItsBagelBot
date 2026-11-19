// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import "context"

const freezeCopy = `INSERT INTO loyalty_freeze_balances (user_id, freeze_id, viewer_id, points, watch_seconds, login, viewer_name)
	SELECT user_id, ?, viewer_id, points, watch_seconds, COALESCE(viewer_login, ''), COALESCE(viewer_name, '')
	FROM balances WHERE user_id=?`

var freezeTables = []string{
	`CREATE TABLE IF NOT EXISTS loyalty_freezes (
		user_id BIGINT PRIMARY KEY,
		freeze_id VARCHAR(32) NOT NULL DEFAULT '',
		as_of_ms BIGINT NOT NULL DEFAULT 0,
		expires_at_ms BIGINT NOT NULL DEFAULT 0,
		holders VARCHAR(2048) NOT NULL DEFAULT '{}',
		block_transfer INTEGER NOT NULL DEFAULT 0,
		close_games INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS loyalty_freeze_epochs (
		user_id BIGINT NOT NULL, freeze_id VARCHAR(32) NOT NULL, ends_at_ms BIGINT NOT NULL,
		as_of_ms BIGINT NOT NULL,
		pruning INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, freeze_id))`,
	`CREATE TABLE IF NOT EXISTS loyalty_freeze_balances (
		user_id BIGINT NOT NULL, freeze_id VARCHAR(32) NOT NULL, viewer_id BIGINT NOT NULL,
		points BIGINT NOT NULL, watch_seconds BIGINT UNSIGNED NOT NULL,
		login VARCHAR(64) NOT NULL DEFAULT '', viewer_name VARCHAR(64) NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, freeze_id, viewer_id))`,
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
		{watchHistoryTable{name: "loyalty_freeze_balances"}, watchHistoryIndex{suffix: "rank", columns: "user_id,freeze_id,points DESC,viewer_id"}},
		{watchHistoryTable{name: "loyalty_freeze_balances"}, watchHistoryIndex{suffix: "login", columns: "user_id,freeze_id,login"}},
		{watchHistoryTable{name: "loyalty_freeze_epochs"}, watchHistoryIndex{suffix: "ends", columns: "ends_at_ms"}},
	}
	for _, entry := range indexes {
		if err := r.ensureWatchIndex(ctx, entry.table, entry.index); err != nil {
			return err
		}
	}
	return nil
}
