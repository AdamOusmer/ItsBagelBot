// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"math"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"
)

type Wager struct {
	UserID, ViewerID uint64
	Amount           int64
	Won              bool
}

type WagerOutcome struct {
	Balance       *ent.Balance
	Found         bool
	Applied       bool
	LimitExceeded bool
}

// BalanceWager settles the net result in one guarded UPDATE. A win adds the
// stake; a loss subtracts it. Neither insufficient funds nor a winning result
// outside signed BIGINT can debit the wager before refusing its payout.
func (r *Loyalty) BalanceWager(ctx context.Context, wager Wager) (WagerOutcome, error) {
	if wager.UserID == 0 || wager.ViewerID == 0 {
		return WagerOutcome{}, fmt.Errorf("%w: user_id/viewer_id", ErrInvalidInput)
	}
	if wager.Amount <= 0 {
		return WagerOutcome{}, fmt.Errorf("%w: wager amount", ErrInvalidInput)
	}
	applied, err := r.applyWager(ctx, wager)
	if err != nil {
		return WagerOutcome{}, err
	}
	row, found, err := r.BalanceGet(ctx, wager.UserID, wager.ViewerID)
	if err != nil {
		return WagerOutcome{}, err
	}
	outcome := WagerOutcome{Balance: row, Found: found, Applied: applied}
	if found && !applied {
		outcome.LimitExceeded = wager.exceedsHeadroom(row.Points)
	}
	return outcome, nil
}

func (r *Loyalty) applyWager(ctx context.Context, wager Wager) (bool, error) {
	updated := 0
	err := db.WithExec(ctx, func(ctx context.Context) error {
		update := r.client.Balance.Update().Where(balance.UserIDEQ(wager.UserID), balance.ViewerIDEQ(wager.ViewerID), balance.PointsGTE(wager.Amount))
		delta := -wager.Amount
		if wager.Won {
			update.Where(balance.PointsLTE(math.MaxInt64 - wager.Amount))
			delta = wager.Amount
		}
		var err error
		updated, err = update.AddPoints(delta).Save(ctx)
		return err
	})
	return updated != 0, err
}

func (wager Wager) exceedsHeadroom(points int64) bool {
	if !wager.Won || points < wager.Amount {
		return false
	}
	return points > math.MaxInt64-wager.Amount
}
