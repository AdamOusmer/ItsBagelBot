// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

type BalanceAdjustment struct {
	UserID, ViewerID uint64
	ViewerLogin      string
	Value            int64
	Absolute         bool
}

func BalanceView(row *ent.Balance) *loyaltyrpc.Balance {
	return &loyaltyrpc.Balance{
		ViewerID:     strconv.FormatUint(row.ViewerID, 10),
		ViewerLogin:  row.ViewerLogin,
		ViewerName:   row.ViewerName,
		Points:       row.Points,
		PointsExact:  strconv.FormatInt(row.Points, 10),
		WatchSeconds: row.WatchSeconds,
	}
}

func (r *Loyalty) BalanceAdjustViewer(ctx context.Context, target BalanceAdjustment) (*ent.Balance, bool, error) {
	target.ViewerLogin = validate.NormalizeLogin(target.ViewerLogin)
	if target.UserID == 0 || target.ViewerID == 0 {
		return nil, false, fmt.Errorf("%w: user_id/viewer_id", ErrInvalidInput)
	}
	if target.ViewerLogin == "" || len(target.ViewerLogin) > maxCounterName {
		return nil, false, fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	var row *ent.Balance
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
			_, update, err := r.prepareBalanceAdjustment(ctx, tx.Client(), target)
			if err != nil {
				return err
			}
			row, err = update.SetViewerLogin(target.ViewerLogin).Save(ctx)
			return err
		})
	})
	if err != nil {
		return nil, false, err
	}
	r.publishBalanceChange(target.UserID, target.ViewerID)
	return row.Unwrap(), true, nil
}

func (r *Loyalty) prepareBalanceAdjustment(ctx context.Context, client *ent.Client, target BalanceAdjustment) (*ent.Balance, *ent.BalanceUpdateOne, error) {
	if err := client.Balance.Create().SetUserID(target.UserID).SetViewerID(target.ViewerID).
		SetViewerLogin(target.ViewerLogin).OnConflictColumns(balance.FieldUserID, balance.FieldViewerID).
		Ignore().Exec(ctx); err != nil {
		return nil, nil, err
	}
	row, err := r.lockedAdjustmentBalance(ctx, client, target)
	if err != nil {
		return nil, nil, err
	}
	points, err := adjustedPoints(row.Points, target)
	if err != nil {
		return nil, nil, err
	}
	return row, client.Balance.UpdateOneID(row.ID).SetPoints(points), nil
}

func (r *Loyalty) lockedAdjustmentBalance(ctx context.Context, client *ent.Client, target BalanceAdjustment) (*ent.Balance, error) {
	query := client.Balance.Query().Where(balance.UserIDEQ(target.UserID), balance.ViewerIDEQ(target.ViewerID))
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
		return current, nil
	}
	return max(current+target.Value, 0), nil
}
