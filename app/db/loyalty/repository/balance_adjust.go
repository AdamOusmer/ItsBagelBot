// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"math"
	"strings"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"
)

type BalanceAdjustment struct {
	UserID, ViewerID uint64
	ViewerLogin      string
	Value            int64
	Absolute         bool
}

// BalanceAdjustViewer uses a resolved Twitch identity and can create a first
// balance. Creating or locking the row and adjusting it share a transaction:
// simultaneous earnings cannot be overwritten by an earlier balance snapshot.
func (r *Loyalty) BalanceAdjustViewer(ctx context.Context, target BalanceAdjustment) (*ent.Balance, bool, error) {
	target.ViewerLogin = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(target.ViewerLogin), "@"))
	if target.UserID == 0 || target.ViewerID == 0 {
		return nil, false, fmt.Errorf("%w: user_id/viewer_id", ErrInvalidInput)
	}
	if target.ViewerLogin == "" || len(target.ViewerLogin) > maxCounterName {
		return nil, false, fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	var row *ent.Balance
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, r.client, func(tx *ent.Tx) error {
			var err error
			row, err = r.applyBalanceAdjustment(ctx, tx, target)
			return err
		})
	})
	if err != nil {
		return nil, false, err
	}
	return row.Unwrap(), true, nil
}

func (r *Loyalty) applyBalanceAdjustment(ctx context.Context, tx *ent.Tx, target BalanceAdjustment) (*ent.Balance, error) {
	if err := tx.Balance.Create().SetUserID(target.UserID).SetViewerID(target.ViewerID).
		SetViewerLogin(target.ViewerLogin).OnConflictColumns(balance.FieldUserID, balance.FieldViewerID).
		Ignore().Exec(ctx); err != nil {
		return nil, err
	}
	row, err := r.lockedAdjustmentBalance(ctx, tx, target)
	if err != nil {
		return nil, err
	}
	points, err := adjustedPoints(row.Points, target)
	if err != nil {
		return nil, err
	}
	return tx.Balance.UpdateOneID(row.ID).SetPoints(points).SetViewerLogin(target.ViewerLogin).Save(ctx)
}

func (r *Loyalty) lockedAdjustmentBalance(ctx context.Context, tx *ent.Tx, target BalanceAdjustment) (*ent.Balance, error) {
	query := tx.Balance.Query().Where(balance.UserIDEQ(target.UserID), balance.ViewerIDEQ(target.ViewerID))
	// SQLite's preceding INSERT reserves the writer lock. MySQL also locks the
	// conflicted row; FOR UPDATE makes the authoritative read explicit.
	if r.dialect != "sqlite3" {
		query.ForUpdate()
	}
	return query.Only(ctx)
}

func adjustedPoints(current int64, target BalanceAdjustment) (int64, error) {
	if target.Absolute {
		return max(target.Value, 0), nil
	}
	if target.Value > 0 && current > math.MaxInt64-target.Value {
		return 0, fmt.Errorf("%w: points exceed signed BIGINT range", ErrInvalidInput)
	}
	if target.Value < 0 && current < 0 {
		return 0, nil
	}
	return max(current+target.Value, 0), nil
}
