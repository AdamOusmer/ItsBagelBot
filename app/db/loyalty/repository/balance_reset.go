// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"
)

func (r *Loyalty) BalanceReset(ctx context.Context, userID uint64) (int64, error) {
	if userID == 0 {
		return 0, fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	var reset int
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error {
			var err error
			reset, err = r.client.Balance.Update().
				Where(balance.UserIDEQ(userID), balance.PointsNEQ(0)).
				SetPoints(0).Save(ctx)
			return err
		})
	})
	return int64(reset), err
}
