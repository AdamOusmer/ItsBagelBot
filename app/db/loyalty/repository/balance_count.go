// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/pkg/db"
)

type BalanceMinimums struct {
	Points, WatchSeconds int64
}

func (r *Loyalty) BalanceCountAtLeast(ctx context.Context, channel uint64, id SnapshotID, mins BalanceMinimums) (int64, error) {
	if mins.Points < 0 || mins.WatchSeconds < 0 {
		return 0, ErrInvalidInput
	}
	return db.WithRead(ctx, func(ctx context.Context) (int64, error) {
		if id != "" {
			return r.countSnapshot(ctx, snapshotKey{channel, id}, mins)
		}
		return r.countRows(ctx, liveRankSource(channel), mins.filter())
	})
}

func (mins BalanceMinimums) filter() rankFilter {
	var filter rankFilter
	if mins.Points > 0 {
		filter.where = " AND points >= ?"
		filter.args = append(filter.args, mins.Points)
	}
	if mins.WatchSeconds > 0 {
		filter.where += " AND watch_seconds >= ?"
		filter.args = append(filter.args, mins.WatchSeconds)
	}
	return filter
}

func (r *Loyalty) countSnapshot(ctx context.Context, key snapshotKey, mins BalanceMinimums) (int64, error) {
	filter := mins.filter()
	query := "SELECT COUNT(b.viewer_id)" + privateSnapshotJoin + filter.where + privateSnapshotScope + " GROUP BY s.user_id,s.snapshot_id"
	args := append(filter.args, key.userID, key.id, time.Now().UnixMilli())
	var count int64
	err := r.sqldb.QueryRowContext(ctx, query, args...).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSnapshotGone
	}
	return count, err
}
