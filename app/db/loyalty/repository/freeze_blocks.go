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

type freezeBlock struct {
	userID uint64
	kind   freezeBlockKind
}

type freezeBlockKind uint8

const (
	freezeTransfers freezeBlockKind = iota
	freezeGames
)

func (b freezeBlock) check(ctx context.Context, tx *sql.Tx) error {
	var flags FreezeFlags
	err := tx.QueryRowContext(ctx, `SELECT block_transfer, close_games FROM loyalty_freezes WHERE user_id=? AND expires_at_ms>?`, b.userID, time.Now().UnixMilli()).
		Scan(&flags.BlockTransfer, &flags.CloseGames)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return flags.refusal(b.kind)
}

func (f FreezeFlags) refusal(kind freezeBlockKind) error {
	if kind == freezeTransfers && f.BlockTransfer {
		return ErrFrozenTransfers
	}
	if kind == freezeGames && f.CloseGames {
		return ErrFrozenGames
	}
	return nil
}

type balanceMutation func(context.Context, *ent.Client) error

func (r *Loyalty) withFreezeCheck(ctx context.Context, block freezeBlock, fn balanceMutation) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error {
			return r.checkAndMutateBalance(ctx, block, fn)
		})
	})
}

func (r *Loyalty) checkAndMutateBalance(ctx context.Context, block freezeBlock, fn balanceMutation) error {
	tx, err := r.sqldb.BeginTx(ctx, db.ReadCommittedTx(r.dialect))
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := block.check(ctx, tx); err != nil {
		return err
	}
	client := ent.NewClient(ent.Driver(db.EntTransactionDriver(r.dialect, tx)))
	client.Balance.Use(r.client.Balance.Hooks()...)
	client.Balance.Intercept(r.client.Balance.Interceptors()...)
	if err := fn(ctx, client); err != nil {
		return err
	}
	return tx.Commit()
}
