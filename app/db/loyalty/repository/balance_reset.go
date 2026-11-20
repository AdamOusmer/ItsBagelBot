// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (r *Loyalty) BalanceReset(ctx context.Context, userID uint64) (int64, error) {
	if userID == 0 {
		return 0, fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	return withStandingsRow(ctx, r, userID, func(ctx context.Context, tx *sql.Tx, state FreezeState) (int64, error) {
		now := time.Now()
		if state.Active(now) {
			return 0, ErrFrozen
		}
		result, err := tx.ExecContext(ctx, `UPDATE balances SET points=0, updated_at=? WHERE user_id=? AND points<>0`, now, userID)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	})
}
