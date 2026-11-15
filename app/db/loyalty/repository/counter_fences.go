// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"strings"

	"ItsBagelBot/pkg/db"
)

func (r *Loyalty) lockCounterUsers(ctx context.Context, tx *sql.Tx, ids []uint64) (map[uint64]bool, error) {
	retired := make(map[uint64]bool, len(ids))
	if !r.watchReady.Load() {
		return retired, nil
	}
	rows := counterFenceRows(ids)
	suffix := " ON DUPLICATE KEY UPDATE user_id=user_id"
	if r.dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id) DO NOTHING"
	}
	spec := upsertSpec{insert: "INSERT INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES ", placeholder: "(?, 0, 0)", suffix: suffix}
	if err := writeCounterRows(ctx, tx, spec, rows); err != nil {
		return nil, err
	}
	chunks, err := db.StatementRowChunks(rows, 256)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		if err := readCounterFences(ctx, tx, chunk, retired); err != nil {
			return nil, err
		}
	}
	return retired, nil
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

func readCounterFences(ctx context.Context, tx *sql.Tx, chunk [][]any, retired map[uint64]bool) error {
	args := make([]any, 0, len(chunk))
	for _, row := range chunk {
		args = append(args, row[0])
	}
	query := "SELECT user_id,deleted FROM loyalty_account_fences WHERE user_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + ") ORDER BY user_id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var deleted bool
		if err := rows.Scan(&id, &deleted); err != nil {
			return err
		}
		retired[id] = deleted
	}
	return rows.Err()
}
