// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

// The per-viewer points ledger. Split out of queries.go, which had grown to
// hold two unrelated APIs sharing only the ent client: this guarded-arithmetic
// ledger, and the broadcaster-authored named counters with their own name and
// scope grammar. The counter half stays in queries.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ItsBagelBot/app/loyalty/ent"
	"ItsBagelBot/app/loyalty/ent/balance"
	"ItsBagelBot/app/loyalty/ent/counter"
	"ItsBagelBot/app/loyalty/ent/counterentry"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"
)

// defaultTopLimit bounds a leaderboard read when the caller sent no (or a
// silly) limit.
const (
	defaultTopLimit = 10
	maxTopLimit     = 100
)

// ErrInvalidInput marks trust-boundary rejections; the RPC layer maps it to a
// "bad request" reply instead of a generic failure.
var ErrInvalidInput = errors.New("invalid input")

// BalanceGet returns one viewer's standing. A missing row is (zero, false, nil).
func (r *Loyalty) BalanceGet(ctx context.Context, userID, viewerID uint64) (*ent.Balance, bool, error) {
	return getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerIDEQ(viewerID)).
			Only(ctx)
	})
}

// Top returns the channel's top standings by points.
func (r *Loyalty) Top(ctx context.Context, userID uint64, limit int) ([]*ent.Balance, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID)).
			Order(balance.ByPoints(entsql.OrderDesc()), balance.ByViewerID()).
			Limit(clampLimit(limit)).
			All(ctx)
	})
}

// BalanceAdjust writes a viewer's points by login (a mod's "!points set/add
// @user" — chat knows the target's login, not their id). absolute sets the
// value; otherwise value is a delta. The row must already exist (any accrual
// creates it); an unseen login is (nil, false, nil) so the caller can answer
// "haven't seen them yet" instead of inventing an id-less row. Renames can
// leave several rows carrying one old login; the freshest wins.
func (r *Loyalty) BalanceAdjust(ctx context.Context, userID uint64, viewerLogin string, value int64, absolute bool) (*ent.Balance, bool, error) {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(viewerLogin), "@"))
	if login == "" {
		return nil, false, fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	row, found, err := getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
	if err != nil || !found {
		return nil, found, err
	}
	return row, true, db.WithExec(ctx, func(ctx context.Context) error {
		upd := r.client.Balance.UpdateOneID(row.ID)
		if absolute {
			row.Points = value
			upd.SetPoints(value)
		} else {
			row.Points += value
			upd.AddPoints(value)
		}
		return upd.Exec(ctx)
	})
}

// BalanceSpend conditionally debits amount points from a viewer, addressed by
// login (chat wagers know the target's login, not their id). The guard lives
// in the UPDATE's WHERE clause — points >= amount — not in a prior read, so a
// concurrent second spend racing the same row can never drive points
// negative: whichever UPDATE lands second no longer matches and is refused.
// spent=false with found=false means the channel never accrued for that
// login; spent=false with found=true means insufficient points (Balance then
// carries what they actually hold). amount must be positive: a zero or
// negative "spend" is a caller bug, not a refund path.
func (r *Loyalty) BalanceSpend(ctx context.Context, userID uint64, viewerLogin string, amount int64) (*ent.Balance, bool, bool, error) {
	login, err := normalizeSpendTarget(viewerLogin, amount)
	if err != nil {
		return nil, false, false, err
	}
	row, found, err := getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerLoginEQ(login)).
			Order(balance.ByUpdatedAt(entsql.OrderDesc()), balance.ByViewerID()).
			First(ctx)
	})
	if err != nil || !found {
		return nil, false, found, err
	}
	n, err := r.client.Balance.Update().
		Where(balance.IDEQ(row.ID), balance.PointsGTE(amount)).
		AddPoints(-amount).
		Save(ctx)
	if err != nil {
		return nil, true, false, err
	}
	if n == 0 {
		// Refused: another write moved the balance after our read, so the
		// snapshot above is stale. Report what the row holds now.
		fresh, ferr := r.client.Balance.Get(ctx, row.ID)
		if ferr != nil {
			return nil, true, false, ferr
		}
		return fresh, true, false, nil
	}
	spent, err := r.client.Balance.Get(ctx, row.ID)
	if err != nil {
		return nil, true, true, err
	}
	return spent, true, true, nil
}

// normalizeSpendTarget validates a spend request and returns the lower-cased
// login the ledger addresses.
func normalizeSpendTarget(viewerLogin string, amount int64) (string, error) {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(viewerLogin), "@"))
	if login == "" || amount <= 0 {
		return "", fmt.Errorf("%w: viewer_login/amount", ErrInvalidInput)
	}
	return login, nil
}

// errInsufficient marks the guarded debit that matched no row — an internal
// sentinel unwound out of the transaction, never surfaced as itself.
var errInsufficient = errors.New("insufficient points")

// TransferOutcome is one balance.transfer's result: the sender's and
// recipient's standings after the move (To is non-nil only when moved).
type TransferOutcome struct {
	From, To *ent.Balance
}

// BalanceTransfer moves amount points from one viewer to another in one
// transaction: the debit is guarded by points >= amount (same shape as
// BalanceSpend, so a concurrent second spend can never drive the sender
// negative), and the credit lands on the recipient row resolved by login —
// which must already exist, exactly like a mod grant. found=false means the
// channel never accrued for sender or target; moved=false with found=true
// means the sender could not cover it (From then carries their actual
// standing). A self-transfer is refused up front: the net-zero move has no
// caller story, so it is input, not ledger state.
func (r *Loyalty) BalanceTransfer(ctx context.Context, userID, fromViewerID uint64, targetLogin string, amount int64) (*TransferOutcome, bool, error) {
	sender, recipient, found, err := r.transferParties(ctx, userID, fromViewerID, targetLogin, amount)
	if err != nil || !found {
		return nil, found, err
	}

	err = r.moveBalance(ctx, sender.ID, recipient.ID, amount)
	if errors.Is(err, errInsufficient) {
		// The guarded debit matched no row: report the sender's real standing
		// rather than the pre-flight read, which a concurrent spend may have
		// already invalidated.
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

// transferParties resolves and validates both sides of a move. found=false
// means one of them never accrued in this channel; a self-transfer is input,
// not ledger state, so it errors rather than performing a net-zero write.
func (r *Loyalty) transferParties(ctx context.Context, userID, fromViewerID uint64, targetLogin string, amount int64) (sender, recipient *ent.Balance, found bool, err error) {
	login, err := normalizeSpendTarget(targetLogin, amount)
	if err != nil {
		return nil, nil, false, err
	}
	if fromViewerID == 0 {
		return nil, nil, false, fmt.Errorf("%w: from_viewer_id", ErrInvalidInput)
	}

	sender, found, err = getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerIDEQ(fromViewerID)).
			Only(ctx)
	})
	if err != nil || !found {
		return nil, nil, found, err
	}
	recipient, found, err = getOptional(ctx, func(ctx context.Context) (*ent.Balance, error) {
		return r.client.Balance.Query().
			Where(balance.UserIDEQ(userID), balance.ViewerLoginEQ(login)).
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
// the points >= amount predicate (the same shape BalanceSpend uses) so a
// concurrent second spend can never drive the sender negative; matching no row
// unwinds the transaction with errInsufficient, leaving the credit unapplied.
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

// clampLimit bounds a caller-provided page size, defaulting a missing one.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultTopLimit
	}
	return min(limit, maxTopLimit)
}

// SetTarget addresses one stored bucket of an entry-scoped counter (a viewer
// for the viewer scopes, a command bucket for command scope). ViewerLogin
// optionally stamps the bucket's display identity — the dashboard's manual
// add knows the typed username; a later bump refreshes it like any other.
type SetTarget struct {
	ViewerID    uint64
	Command     string
	ViewerLogin string
}

// withTx runs fn inside one ent transaction, committing on success and
// rolling back on error.
func withTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DeleteAllForUser removes every loyalty row of a deleted broadcaster account.
func (r *Loyalty) DeleteAllForUser(ctx context.Context, userID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		if _, err := r.client.Balance.Delete().Where(balance.UserIDEQ(userID)).Exec(ctx); err != nil {
			return err
		}
		if _, err := r.client.CounterEntry.Delete().Where(counterentry.UserIDEQ(userID)).Exec(ctx); err != nil {
			return err
		}
		_, err := r.client.Counter.Delete().Where(counter.UserIDEQ(userID)).Exec(ctx)
		return err
	})
}

// getOptional runs one Only-style query through the DB gate and maps ent's
// not-found to (zero, false, nil).
func getOptional[T any](ctx context.Context, fn func(context.Context) (*T, error)) (*T, bool, error) {
	row, err := db.WithQuery(ctx, fn)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return row, true, nil
}
