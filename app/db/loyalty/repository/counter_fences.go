// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"strings"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

type counterFence struct {
	retired  bool
	promoted bool
}

func (r *Loyalty) lockCounterUsers(ctx context.Context, tx *sql.Tx, ids []uint64) (map[uint64]counterFence, error) {
	fences := make(map[uint64]counterFence, len(ids))
	chunks, err := db.StatementRowChunks(counterFenceRows(ids), 256)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		if err := r.lockCounterUserChunk(ctx, tx, chunk, fences); err != nil {
			return nil, err
		}
	}
	return fences, nil
}

func counterFenceRows(ids []uint64) [][]any {
	var rows [][]any
	for _, id := range ids {
		if id != 0 {
			rows = append(rows, []any{id})
		}
	}
	return rows
}

func (r *Loyalty) lockCounterUserChunk(ctx context.Context, tx *sql.Tx, chunk [][]any, fences map[uint64]counterFence) error {
	if err := r.readCounterUsers(ctx, tx, chunk, fences); err != nil {
		return err
	}
	var missing [][]any
	for _, row := range chunk {
		if _, found := fences[row[0].(uint64)]; !found {
			missing = append(missing, row)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	spec := upsertSpec{
		insert:      r.insertIgnore() + " INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES ",
		placeholder: "(?, 0, 0)",
	}
	if _, err := writeCounterRows(ctx, tx, spec, missing); err != nil {
		return err
	}
	return r.readCounterUsers(ctx, tx, missing, fences)
}

func (r *Loyalty) readCounterUsers(ctx context.Context, tx *sql.Tx, chunk [][]any, fences map[uint64]counterFence) error {
	suffix := " FOR SHARE OF f"
	if r.dialect == "sqlite3" {
		suffix = ""
	}
	args := make([]any, 0, len(chunk))
	for _, row := range chunk {
		args = append(args, row[0])
	}
	query := "SELECT f.user_id,f.deleted,(p.user_id IS NOT NULL) FROM loyalty_account_fences f " +
		"LEFT JOIN counters p ON p.user_id=f.user_id AND p.name='" + data.CounterTrialPromoted + "' " +
		"WHERE f.user_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + ") ORDER BY f.user_id" + suffix
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var fence counterFence
		if err := rows.Scan(&id, &fence.retired, &fence.promoted); err != nil {
			return err
		}
		fences[id] = fence
	}
	return rows.Err()
}
