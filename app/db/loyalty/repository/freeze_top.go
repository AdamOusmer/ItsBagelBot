// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/pkg/db"
)

const frozenBalances = "loyalty_freeze_balances"
const frozenScope = `user_id=? AND snapshot_id=? AND EXISTS (
	SELECT 1 FROM loyalty_snapshots WHERE user_id=? AND snapshot_id=? AND owner IS NULL AND pruning=0)`
const frozenSelect = "SELECT user_id,viewer_id,points,watch_seconds,login,viewer_name FROM " + frozenBalances + " WHERE " + frozenScope

func (e snapshotKey) rankSource() rankSource {
	return rankSource{table: frozenBalances, index: "idx_loyalty_freeze_balances_rank", scope: frozenScope, args: e.publicScopeArgs()}
}

func (e snapshotKey) publicScopeArgs() []any {
	return []any{e.userID, e.id, e.userID, e.id}
}

func (r *Loyalty) FrozenTopPage(ctx context.Context, userID uint64, id SnapshotID, q TopQuery) (TopPage, error) {
	key := snapshotKey{userID, id}
	return db.WithRead(ctx, func(ctx context.Context) (TopPage, error) {
		if q.searchesViewer() {
			return r.frozenViewerPage(ctx, key, q)
		}
		return r.frozenRankedPage(ctx, key, q)
	})
}

func (r *Loyalty) frozenRankedPage(ctx context.Context, key snapshotKey, q TopQuery) (TopPage, error) {
	var page TopPage
	offset := clampOffset(q.Offset)
	if err := r.countFrozenPage(ctx, key, offset, &page); err != nil {
		return TopPage{}, err
	}
	rows, err := r.sqldb.QueryContext(ctx, frozenSelect+" ORDER BY points DESC,viewer_id LIMIT ? OFFSET ?", append(key.publicScopeArgs(), clampLimit(q.Limit), offset)...)
	if err != nil {
		return TopPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanFrozenBalance(rows)
		if err != nil {
			return TopPage{}, err
		}
		page.Rows = append(page.Rows, RankedBalance{Balance: row})
	}
	positionRanks(offset, page.Rows)
	return page, rows.Err()
}

func (r *Loyalty) countFrozenPage(ctx context.Context, key snapshotKey, offset int, page *TopPage) error {
	if offset == 0 {
		return nil
	}
	var err error
	page.Total, err = r.countRows(ctx, key.rankSource(), rankFilter{})
	return err
}

func (r *Loyalty) frozenViewerPage(ctx context.Context, key snapshotKey, q TopQuery) (TopPage, error) {
	row, err := r.lookupFrozen(ctx, key, q)
	if errors.Is(err, sql.ErrNoRows) {
		return TopPage{}, nil
	}
	if err != nil {
		return TopPage{}, err
	}
	rank, err := r.boardRank(ctx, key.rankSource(), row)
	return TopPage{Rows: []RankedBalance{{Balance: row, Rank: rank}}, Total: 1}, err
}

func (r *Loyalty) lookupFrozen(ctx context.Context, key snapshotKey, q TopQuery) (*ent.Balance, error) {
	if q.ViewerID != 0 {
		return scanFrozenBalance(r.sqldb.QueryRowContext(ctx, frozenSelect+" AND viewer_id=?", append(key.publicScopeArgs(), q.ViewerID)...))
	}
	return scanFrozenBalance(r.sqldb.QueryRowContext(ctx, frozenSelect+" AND login=? ORDER BY viewer_id LIMIT 1", append(key.publicScopeArgs(), q.ViewerLogin)...))
}

type balanceScanner interface {
	Scan(...any) error
}

func scanFrozenBalance(scanner balanceScanner) (*ent.Balance, error) {
	row := new(ent.Balance)
	err := scanner.Scan(&row.UserID, &row.ViewerID, &row.Points, &row.WatchSeconds, &row.ViewerLogin, &row.ViewerName)
	return row, err
}

func (r *Loyalty) FreezeEpoch(ctx context.Context, userID uint64, id SnapshotID) (FreezeState, error) {
	return db.WithRead(ctx, func(ctx context.Context) (FreezeState, error) {
		var asOf, end int64
		err := r.sqldb.QueryRowContext(ctx, `SELECT as_of_ms, expires_at_ms FROM loyalty_snapshots WHERE user_id=? AND snapshot_id=? AND pruning=0 AND owner IS NULL`, userID, id).Scan(&asOf, &end)
		if errors.Is(err, sql.ErrNoRows) {
			return FreezeState{}, ErrStaleFreezePin
		}
		return FreezeState{SnapshotID: id, AsOf: time.UnixMilli(asOf), ExpiresAt: time.UnixMilli(end)}, err
	})
}
