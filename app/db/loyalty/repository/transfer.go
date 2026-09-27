// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"math"

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
	UserID         uint64
	FromViewerID   uint64
	TargetLogin    string
	TargetViewerID uint64
	Amount         int64
}

func (r *Loyalty) BalanceTransfer(ctx context.Context, t Transfer) (*TransferOutcome, bool, error) {
	sender, recipient, found, err := r.transferParties(ctx, t)
	if err != nil || !found {
		return nil, found, err
	}

	if sender.ID == 0 {
		return &TransferOutcome{From: sender}, true, nil
	}
	err = r.moveBalance(ctx, sender.ID, recipient, t.Amount)
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
	return r.transferOutcome(ctx, sender.ID, recipient.UserID, recipient.ViewerID)
}

func (r *Loyalty) transferParties(ctx context.Context, t Transfer) (sender, recipient *ent.Balance, found bool, err error) {
	login, err := validTransfer(t)
	if err != nil {
		return nil, nil, false, err
	}

	sender, found, err = getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(t.UserID), balance.ViewerIDEQ(t.FromViewerID)).
			Only(ctx)
	})
	if err != nil {
		return nil, nil, false, err
	}
	if !found {
		return &ent.Balance{UserID: t.UserID, ViewerID: t.FromViewerID}, nil, true, nil
	}
	recipient, found, err = r.transferRecipient(ctx, t, login)
	if err != nil || !found {
		return nil, nil, found, err
	}
	if recipient.ViewerID == sender.ViewerID {
		return nil, nil, true, fmt.Errorf("%w: self transfer", ErrInvalidInput)
	}
	return sender, recipient, true, nil
}

func validTransfer(t Transfer) (string, error) {
	login, err := normalizeSpendTarget(t.TargetLogin, t.Amount)
	if err != nil {
		return "", err
	}
	if t.TargetViewerID != 0 && t.TargetViewerID == t.FromViewerID {
		return "", fmt.Errorf("%w: self transfer", ErrInvalidInput)
	}
	if t.FromViewerID == 0 {
		return "", fmt.Errorf("%w: from_viewer_id", ErrInvalidInput)
	}
	return login, nil
}

// A resolved Twitch ID allows a first transfer to create the recipient's balance.
// Legacy callers without an ID can only transfer to an existing balance.
func (r *Loyalty) transferRecipient(ctx context.Context, t Transfer, login string) (*ent.Balance, bool, error) {
	if t.TargetViewerID != 0 {
		return &ent.Balance{UserID: t.UserID, ViewerID: t.TargetViewerID, ViewerLogin: login}, true, nil
	}
	return getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(t.UserID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
}

// The points >= amount predicate stops a concurrent spend driving the sender negative.
// Creating or crediting the recipient shares the debit transaction, including rollback.
func (r *Loyalty) moveBalance(ctx context.Context, senderID int, recipient *ent.Balance, amount int64) error {
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
			return creditTransfer(ctx, tx, recipient, amount)
		})
	})
}

// The conflict write locks an existing recipient before checking integer capacity.
// No floating SQL arithmetic is used at the signed BIGINT boundary.
func creditTransfer(ctx context.Context, tx *ent.Tx, recipient *ent.Balance, amount int64) error {
	if err := tx.Balance.Create().
		SetUserID(recipient.UserID).
		SetViewerID(recipient.ViewerID).
		SetViewerLogin(recipient.ViewerLogin).
		OnConflictColumns(balance.FieldUserID, balance.FieldViewerID).
		Ignore().Exec(ctx); err != nil {
		return err
	}
	row, err := tx.Balance.Query().Where(balance.UserIDEQ(recipient.UserID), balance.ViewerIDEQ(recipient.ViewerID)).Only(ctx)
	if err != nil {
		return err
	}
	if row.Points > math.MaxInt64-amount {
		return fmt.Errorf("%w: recipient balance exceeds BIGINT", ErrInvalidInput)
	}
	return tx.Balance.UpdateOneID(row.ID).AddPoints(amount).SetViewerLogin(recipient.ViewerLogin).Exec(ctx)
}

func (r *Loyalty) transferOutcome(ctx context.Context, senderID int, userID, recipientID uint64) (*TransferOutcome, bool, error) {
	from, err := r.client.Balance.Get(ctx, senderID)
	if err != nil {
		return nil, true, err
	}
	to, err := r.client.Balance.Query().Where(balance.UserIDEQ(userID), balance.ViewerIDEQ(recipientID)).Only(ctx)
	if err != nil {
		return nil, true, err
	}
	return &TransferOutcome{From: from, To: to}, true, nil
}
