// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/pkg/db"
)

type BalanceThreshold struct {
	Points       int64
	WatchSeconds uint64
}

func (r *Loyalty) CountBalancesAtLeast(ctx context.Context, userID uint64, id FreezeID, min BalanceThreshold) (int64, error) {
	source := liveRankSource(userID)
	if id != "" {
		source = (freezeEpochKey{userID, id}).rankSource()
	}
	return db.WithRead(ctx, func(ctx context.Context) (int64, error) {
		return r.countRows(ctx, source, rankFilter{where: " AND points >= ? AND watch_seconds >= ?", args: []any{min.Points, min.WatchSeconds}})
	})
}
