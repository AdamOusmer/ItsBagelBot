// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

var errInsufficient = errors.New("insufficient points")

type TransferOutcome struct {
	From, To *ent.Balance
}

type Transfer struct {
	UserID       uint64
	FromViewerID uint64
	TargetLogin  string
	Amount       int64
}

func (r *Loyalty) BalanceTransfer(ctx context.Context, t Transfer) (*TransferOutcome, bool, error) {
	sender, recipient, found, err := r.transferParties(ctx, t)
	if err != nil || !found {
		return nil, found, err
	}

	err = r.moveBalance(ctx, sender.ID, recipient.ID, t.Amount)
	if errors.Is(err, errInsufficient) {
		fresh, ferr := r.client.Balance.Get(ctx, sender.ID)
		if ferr != nil {
			return nil, true, ferr
		}
		return &TransferOutcome{From: fresh}, true, nil
	}
	if err != nil {
		return nil, true, err
	}
	return r.transferOutcome(ctx, sender.ID, recipient.ID)
}

func (r *Loyalty) transferParties(ctx context.Context, t Transfer) (sender, recipient *ent.Balance, found bool, err error) {
	login, err := normalizeSpendTarget(t.TargetLogin, t.Amount)
	if err != nil {
		return nil, nil, false, err
	}
	if t.FromViewerID == 0 {
		return nil, nil, false, fmt.Errorf("%w: from_viewer_id", ErrInvalidInput)
	}

	sender, found, err = getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(t.UserID), balance.ViewerIDEQ(t.FromViewerID)).
			Only(ctx)
	})
	if err != nil || !found {
		return nil, nil, found, err
	}
	recipient, found, err = getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(t.UserID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
	if err != nil || !found {
		return nil, nil, found, err
	}
	if recipient.ID == sender.ID {
		return nil, nil, true, fmt.Errorf("%w: self transfer", ErrInvalidInput)
	}
	return sender, recipient, true, nil
}

// The points >= amount predicate stops a concurrent spend driving the sender negative.
func (r *Loyalty) moveBalance(ctx context.Context, senderID, recipientID int, amount int64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return withTx(ctx, r.client, func(tx *ent.Tx) error {
			updated, err := tx.Balance.Update().
				Where(balance.IDEQ(senderID), balance.PointsGTE(amount)).
				AddPoints(-amount).
				Save(ctx)
			if err != nil {
				return err
			}
			if updated == 0 {
				return errInsufficient
			}
			_, err = tx.Balance.Update().
				Where(balance.IDEQ(recipientID)).
				AddPoints(amount).
				Save(ctx)
			return err
		})
	})
}

func (r *Loyalty) transferOutcome(ctx context.Context, senderID, recipientID int) (*TransferOutcome, bool, error) {
	from, err := r.client.Balance.Get(ctx, senderID)
	if err != nil {
		return nil, true, err
	}
	to, err := r.client.Balance.Get(ctx, recipientID)
	if err != nil {
		return nil, true, err
	}
	return &TransferOutcome{From: from, To: to}, true, nil
}
