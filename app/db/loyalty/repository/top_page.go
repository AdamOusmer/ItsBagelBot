// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

const (
	maxTopOffset = 10000
	rankIndex    = "balance_user_id_points_viewer_id"
)

type TopQuery struct {
	Limit       int
	Offset      int
	ViewerID    uint64
	ViewerLogin string
}

func (q TopQuery) searchesViewer() bool {
	return q.ViewerID != 0 || q.ViewerLogin != ""
}

func (q TopQuery) reconcileHint() string {
	if q.ViewerID == 0 {
		return ""
	}
	return q.ViewerLogin
}

type RankedBalance struct {
	*ent.Balance
	Rank int64
}

type TopPage struct {
	Rows  []RankedBalance
	Total int64
}

func clampOffset(offset int) int {
	return max(0, min(offset, maxTopOffset))
}

func (r *Loyalty) TopPage(ctx context.Context, userID uint64, q TopQuery) (TopPage, error) {
	if q.searchesViewer() {
		return db.WithQuery(ctx, func(ctx context.Context) (TopPage, error) {
			return r.viewerPage(ctx, userID, q)
		})
	}
	offset := clampOffset(q.Offset)
	page, err := db.WithQuery(ctx, func(ctx context.Context) (TopPage, error) {
		return r.readTopPage(ctx, userID, offset, clampLimit(q.Limit))
	})
	if err != nil {
		return TopPage{}, err
	}
	positionRanks(offset, page.Rows)
	return page, nil
}

func (r *Loyalty) readTopPage(ctx context.Context, userID uint64, offset, limit int) (TopPage, error) {
	var total int
	if offset > 0 {
		var err error
		if total, err = r.client.Balance.Query().Where(balance.UserIDEQ(userID)).Count(ctx); err != nil {
			return TopPage{}, err
		}
	}
	rows, err := r.client.Balance.Query().
		Where(balance.UserIDEQ(userID)).
		Order(balance.ByPoints(entsql.OrderDesc()), balance.ByViewerID()).
		Offset(offset).
		Limit(limit).
		All(ctx)
	return TopPage{Rows: unranked(rows), Total: int64(total)}, err
}

func (r *Loyalty) viewerPage(ctx context.Context, userID uint64, q TopQuery) (TopPage, error) {
	row, err := r.viewerRow(ctx, userID, q)
	if ent.IsNotFound(err) {
		return TopPage{}, nil
	}
	if err != nil {
		return TopPage{}, err
	}
	if row, err = r.reconcileLogin(ctx, row, q.reconcileHint()); err != nil {
		return TopPage{}, err
	}
	rank, err := r.boardRank(ctx, userID, row)
	if err != nil {
		return TopPage{}, err
	}
	return TopPage{Rows: []RankedBalance{{Balance: row, Rank: rank}}, Total: 1}, nil
}

func (r *Loyalty) viewerRow(ctx context.Context, userID uint64, q TopQuery) (*ent.Balance, error) {
	query := r.client.Balance.Query().Where(balance.UserIDEQ(userID))
	if q.ViewerID != 0 {
		return query.Where(balance.ViewerIDEQ(q.ViewerID)).Only(ctx)
	}
	return query.Where(balance.ViewerLoginEQ(q.ViewerLogin)).
		Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
		First(ctx)
}

func (r *Loyalty) reconcileLogin(ctx context.Context, row *ent.Balance, login string) (*ent.Balance, error) {
	if login == "" || login == row.ViewerLogin {
		return row, nil
	}
	return r.client.Balance.UpdateOneID(row.ID).SetViewerLogin(login).Save(ctx)
}

func (r *Loyalty) boardRank(ctx context.Context, userID uint64, row *ent.Balance) (int64, error) {
	ahead, err := r.countRows(ctx, `user_id = ? AND points > ?`, userID, row.Points)
	if err != nil {
		return 0, err
	}
	tied, err := r.countRows(ctx, `user_id = ? AND points = ? AND viewer_id < ?`, userID, row.Points, row.ViewerID)
	if err != nil {
		return 0, err
	}
	return 1 + ahead + tied, nil
}

func (r *Loyalty) countRows(ctx context.Context, where string, args ...any) (int64, error) {
	source := "balances"
	if r.dialect != "sqlite3" {
		source += " FORCE INDEX (" + rankIndex + ")"
	}
	var n int64
	err := r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+source+` WHERE `+where, args...).Scan(&n)
	return n, err
}

func unranked(rows []*ent.Balance) []RankedBalance {
	out := make([]RankedBalance, len(rows))
	for i, row := range rows {
		out[i] = RankedBalance{Balance: row}
	}
	return out
}

func positionRanks(offset int, rows []RankedBalance) {
	for i := range rows {
		rows[i].Rank = int64(offset + i + 1)
	}
}
