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
const frozenScope = "user_id=? AND freeze_id=?"
const frozenSelect = "SELECT user_id,viewer_id,points,watch_seconds,login,viewer_name FROM " + frozenBalances + " WHERE " + frozenScope

func (e freezeEpochKey) rankSource() rankSource {
	return rankSource{table: frozenBalances, index: "idx_loyalty_freeze_balances_rank", scope: frozenScope, args: []any{e.userID, e.id}}
}

func (r *Loyalty) FrozenTopPage(ctx context.Context, userID uint64, id FreezeID, q TopQuery) (TopPage, error) {
	key := freezeEpochKey{userID, id}
	return db.WithRead(ctx, func(ctx context.Context) (TopPage, error) {
		if q.searchesViewer() {
			return r.frozenViewerPage(ctx, key, q)
		}
		return r.frozenRankedPage(ctx, key, q)
	})
}

func (r *Loyalty) frozenRankedPage(ctx context.Context, key freezeEpochKey, q TopQuery) (TopPage, error) {
	var page TopPage
	offset := clampOffset(q.Offset)
	if err := r.countFrozenPage(ctx, key, offset, &page); err != nil {
		return TopPage{}, err
	}
	rows, err := r.sqldb.QueryContext(ctx, frozenSelect+" ORDER BY points DESC,viewer_id LIMIT ? OFFSET ?", key.userID, key.id, clampLimit(q.Limit), offset)
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

func (r *Loyalty) countFrozenPage(ctx context.Context, key freezeEpochKey, offset int, page *TopPage) error {
	if offset == 0 {
		return nil
	}
	var err error
	page.Total, err = r.countRows(ctx, key.rankSource(), rankFilter{})
	return err
}

func (r *Loyalty) frozenViewerPage(ctx context.Context, key freezeEpochKey, q TopQuery) (TopPage, error) {
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

func (r *Loyalty) lookupFrozen(ctx context.Context, key freezeEpochKey, q TopQuery) (*ent.Balance, error) {
	if q.ViewerID != 0 {
		return scanFrozenBalance(r.sqldb.QueryRowContext(ctx, frozenSelect+" AND viewer_id=?", key.userID, key.id, q.ViewerID))
	}
	return scanFrozenBalance(r.sqldb.QueryRowContext(ctx, frozenSelect+" AND login=? ORDER BY viewer_id LIMIT 1", key.userID, key.id, q.ViewerLogin))
}

type balanceScanner interface {
	Scan(...any) error
}

func scanFrozenBalance(scanner balanceScanner) (*ent.Balance, error) {
	row := new(ent.Balance)
	err := scanner.Scan(&row.UserID, &row.ViewerID, &row.Points, &row.WatchSeconds, &row.ViewerLogin, &row.ViewerName)
	return row, err
}

func (r *Loyalty) FrozenBalance(ctx context.Context, userID, viewerID uint64, id FreezeID) (*ent.Balance, bool, error) {
	row, err := db.WithRead(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.lookupFrozen(ctx, freezeEpochKey{userID, id}, TopQuery{ViewerID: viewerID})
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return row, err == nil, err
}

func (r *Loyalty) FreezeEpoch(ctx context.Context, userID uint64, id FreezeID) (FreezeState, error) {
	return db.WithRead(ctx, func(ctx context.Context) (FreezeState, error) {
		var asOf, end int64
		err := r.sqldb.QueryRowContext(ctx, `SELECT as_of_ms, ends_at_ms FROM loyalty_freeze_epochs WHERE user_id=? AND freeze_id=? AND pruning=0`, userID, id).Scan(&asOf, &end)
		if errors.Is(err, sql.ErrNoRows) {
			return FreezeState{}, ErrStaleFreezePin
		}
		return FreezeState{FreezeID: id, AsOf: time.UnixMilli(asOf), ExpiresAt: time.UnixMilli(end)}, err
	})
}
