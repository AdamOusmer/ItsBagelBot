// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/pkg/db"
)

type BalanceMinimums struct {
	Points, WatchSeconds int64
}

func (r *Loyalty) BalanceCountAtLeast(ctx context.Context, channel uint64, freeze FreezeID, mins BalanceMinimums) (int64, error) {
	if mins.Points < 0 || mins.WatchSeconds < 0 {
		return 0, ErrInvalidInput
	}
	source := liveRankSource(channel)
	if freeze != "" {
		source = (freezeEpochKey{channel, freeze}).rankSource()
	}
	var filter rankFilter
	if mins.Points > 0 {
		filter.where = " AND points >= ?"
		filter.args = append(filter.args, mins.Points)
	}
	if mins.WatchSeconds > 0 {
		filter.where += " AND watch_seconds >= ?"
		filter.args = append(filter.args, mins.WatchSeconds)
	}
	return db.WithRead(ctx, func(ctx context.Context) (int64, error) {
		return r.countRows(ctx, source, filter)
	})
}
