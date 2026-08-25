// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

// Viewer-to-viewer point transfers. Kept out of queries.go: that file already
// carries two APIs sharing only the ent client (the balance ledger and the
// named counters), and folding a third multi-step operation into it pushed it
// under the cohesion bar.

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/loyalty/ent"
	"ItsBagelBot/app/loyalty/ent/balance"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

// errInsufficient marks the guarded debit that matched no row — an internal
// sentinel unwound out of the transaction, never surfaced as itself.
var errInsufficient = errors.New("insufficient points")

// TransferOutcome is one balance.transfer's result: the sender's and
// recipient's standings after the move (To is non-nil only when moved).
type TransferOutcome struct {
	From, To *ent.Balance
}

// Transfer is one requested move. Bundled rather than passed positionally
// because the channel, the sender and the recipient are otherwise three
// interchangeable scalars in a row, two of them bare uint64s.
type Transfer struct {
	UserID       uint64 // the channel the points live in
	FromViewerID uint64 // the sender, always known by id (they are chatting)
	TargetLogin  string // the recipient, as typed in chat
	Amount       int64
}

// BalanceTransfer moves points from one viewer to another in one transaction:
// the debit is guarded by points >= amount (same shape as BalanceSpend, so a
// concurrent second spend can never drive the sender negative), and the credit
// lands on the recipient row resolved by login — which must already exist,
// exactly like a mod grant. found=false means the channel never accrued for
// sender or target; a nil To with found=true means the sender could not cover
// it (From then carries their actual standing).
func (r *Loyalty) BalanceTransfer(ctx context.Context, t Transfer) (*TransferOutcome, bool, error) {
	sender, recipient, found, err := r.transferParties(ctx, t)
	if err != nil || !found {
		return nil, found, err
	}

	err = r.moveBalance(ctx, sender.ID, recipient.ID, t.Amount)
	if errors.Is(err, errInsufficient) {
		// Report the sender's real standing rather than the pre-flight read,
		// which a concurrent spend may already have invalidated.
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

// transferParties resolves and validates both sides. found=false means one of
// them never accrued in this channel. A self-transfer is refused up front: the
// net-zero move has no caller story, so it is input, not ledger state.
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

// moveBalance is the debit and credit as one transaction. The debit carries
// the points >= amount predicate so a concurrent second spend can never drive
// the sender negative; matching no row unwinds the transaction with
// errInsufficient, leaving the credit unapplied.
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

// transferOutcome re-reads both rows after a committed move, so the reply
// carries post-move standings rather than the caller's arithmetic.
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
