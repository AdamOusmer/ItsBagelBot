// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"strings"
)

func (r *Loyalty) preflightWatchBalances(ctx context.Context, tx *sql.Tx, posting watchPosting) error {
	viewers := watchViewers(posting.award)
	for len(viewers) > 0 {
		n := watchChunkSize(viewers)
		if err := r.preflightWatchChunk(ctx, tx, posting, viewers[:n]); err != nil {
			return err
		}
		viewers = viewers[n:]
	}
	return nil
}

func (r *Loyalty) preflightWatchChunk(ctx context.Context, tx *sql.Tx, posting watchPosting, viewers []watchViewer) error {
	selects := make([]string, 0, len(viewers))
	args := make([]any, 0, 2*len(viewers)+1)
	deltas := make(map[uint64]uint64, len(viewers))
	for _, viewer := range viewers {
		selects = append(selects, "SELECT ? AS viewer_id, ? AS award_id")
		args = append(args, viewer.entry.ViewerID, viewer.receipt)
		deltas[viewer.entry.ViewerID] = viewer.entry.WatchSeconds
	}
	args = append(args, posting.award.UserID)
	query := "SELECT b.viewer_id,b.watch_seconds FROM balances b JOIN (" + strings.Join(selects, " UNION ALL ") + ") pending ON b.viewer_id=pending.viewer_id LEFT JOIN loyalty_watch_viewers v ON v.award_id=pending.award_id WHERE b.user_id=? AND v.award_id IS NULL ORDER BY b.viewer_id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return checkWatchBalanceRows(rows, deltas)
}
