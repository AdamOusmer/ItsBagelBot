// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
)

const balanceCreditTable = "loyalty_balance_credits"

var (
	ErrCreditKeyMismatch     = errors.New("credit key has a different payload")
	ErrCreditAccountMismatch = errors.New("credit account instance changed")
	ErrCreditAccountRetired  = errors.New("credit account is retired")
)

type BalanceMutation struct {
	UserID, ViewerID uint64
	ViewerLogin      string
	Value            int64
	Key              string
	AccountCreatedAt int64
}

type BalanceCredit = BalanceMutation
type BalanceDebit = BalanceMutation

type BalanceMutationResult struct {
	Balance  *loyaltyrpc.Balance
	Spent    bool
	Replayed bool
}

type CreditResult = BalanceMutationResult
type DebitResult = BalanceMutationResult

type balanceDirection byte

const (
	balanceCredit balanceDirection = iota
	balanceDebit
)

func (r *Loyalty) EnsureBalanceCreditSchema(ctx context.Context) error {
	stmt := `CREATE TABLE IF NOT EXISTS loyalty_balance_credits (
		user_id BIGINT NOT NULL,
		key_hash BINARY(32) NOT NULL,
		payload_hash BINARY(32) NOT NULL,
		account_created_at BIGINT NOT NULL,
		balance_reply BLOB NOT NULL,
		created_at BIGINT NOT NULL,
		PRIMARY KEY (user_id,key_hash)
	)`
	if r.dialect != "sqlite3" {
		stmt += " ENGINE=InnoDB"
	}
	if _, err := r.sqldb.ExecContext(ctx, stmt); err != nil {
		return err
	}
	if err := r.ensureWatchIndex(ctx, watchHistoryTable{name: balanceCreditTable}, watchHistoryIndex{suffix: "created", columns: "created_at"}); err != nil {
		return err
	}
	r.creditReady.Store(true)
	return nil
}

func (c BalanceMutation) validate() error {
	if c.UserID == 0 || c.ViewerID == 0 {
		return fmt.Errorf("%w: user_id/viewer_id", ErrInvalidInput)
	}
	if c.Value <= 0 || c.AccountCreatedAt < 0 {
		return fmt.Errorf("%w: value/account_created_at", ErrInvalidInput)
	}
	if !balanceKeyPrefixed(c.Key) {
		return fmt.Errorf("%w: key requires a caller prefix and value", ErrInvalidInput)
	}
	if len(validate.NormalizeLogin(c.ViewerLogin)) > maxCounterName {
		return fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	return nil
}

func balanceKeyPrefixed(key string) bool {
	prefix, value, _ := strings.Cut(key, ":")
	return strings.TrimSpace(prefix) != "" && strings.TrimSpace(value) != ""
}

type balanceOperation struct {
	BalanceMutation
	direction            balanceDirection
	keyHash, payloadHash [sha256.Size]byte
}

func (d balanceDirection) receiptKey(key string) [sha256.Size]byte {
	if d == balanceDebit {
		key = ":" + key
	}
	return sha256.Sum256([]byte(key))
}

func (c BalanceMutation) payloadHash(direction balanceDirection) [sha256.Size]byte {
	var body [25]byte
	binary.BigEndian.PutUint64(body[0:8], c.ViewerID)
	binary.BigEndian.PutUint64(body[8:16], uint64(c.Value))
	binary.BigEndian.PutUint64(body[16:24], uint64(c.AccountCreatedAt))
	// Preserve the original credit hash so old and new pods can replay the same receipt.
	if direction == balanceCredit {
		return sha256.Sum256(body[:24])
	}
	body[24] = byte(direction)
	return sha256.Sum256(body[:])
}

func (r *Loyalty) BalanceCredit(ctx context.Context, credit BalanceCredit) (CreditResult, error) {
	return r.mutateBalance(ctx, credit, balanceCredit)
}

func (r *Loyalty) BalanceDebit(ctx context.Context, debit BalanceDebit) (DebitResult, error) {
	return r.mutateBalance(ctx, debit, balanceDebit)
}

func (r *Loyalty) mutateBalance(ctx context.Context, mutation BalanceMutation, direction balanceDirection) (BalanceMutationResult, error) {
	if err := mutation.validate(); err != nil {
		return BalanceMutationResult{}, err
	}
	op := balanceOperation{BalanceMutation: mutation, direction: direction, keyHash: direction.receiptKey(mutation.Key), payloadHash: mutation.payloadHash(direction)}
	result, err := r.commitBalanceMutation(ctx, op)
	if err != nil {
		return result, err
	}
	r.publishBalanceChange(mutation.UserID, mutation.ViewerID)
	return result, nil
}

func (r *Loyalty) commitBalanceMutation(ctx context.Context, op balanceOperation) (BalanceMutationResult, error) {
	var result BalanceMutationResult
	err := retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			result, err = r.balanceMutationTx(ctx, op)
			return err
		})
	})
	if err != nil {
		return BalanceMutationResult{}, err
	}
	return result, nil
}

func (r *Loyalty) balanceMutationTx(ctx context.Context, op balanceOperation) (BalanceMutationResult, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	defer tx.Rollback()
	result, err := r.applyBalanceMutation(ctx, tx, op)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	return result, tx.Commit()
}

func (r *Loyalty) applyBalanceMutation(ctx context.Context, tx *sql.Tx, op balanceOperation) (BalanceMutationResult, error) {
	account, err := r.fence(ctx, tx, op.UserID, 0)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	if account.deleted {
		return BalanceMutationResult{}, ErrCreditAccountRetired
	}
	if !account.admitsInstance(op.AccountCreatedAt) {
		return BalanceMutationResult{}, ErrCreditAccountMismatch
	}
	result, err := r.replayBalanceMutation(ctx, tx, op)
	if err != nil || result.Replayed {
		return result, err
	}
	client := ent.NewClient(ent.Driver(db.EntTransactionDriver(r.dialect, tx)))
	result, err = r.updateKeyedBalance(ctx, client, op)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	return result, op.insertReceipt(ctx, tx, result)
}

func (r *Loyalty) updateKeyedBalance(ctx context.Context, client *ent.Client, op balanceOperation) (BalanceMutationResult, error) {
	adjustment := BalanceAdjustment{UserID: op.UserID, ViewerID: op.ViewerID, ViewerLogin: validate.NormalizeLogin(op.ViewerLogin), Value: op.Value}
	if op.direction == balanceDebit {
		return r.applyBalanceDebit(ctx, client, adjustment)
	}
	row, update, err := r.prepareBalanceAdjustment(ctx, client, adjustment)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	row, err = update.SetViewerLogin(displayUpdate(row.ViewerLogin, adjustment.ViewerLogin)).Save(ctx)
	if err != nil {
		return BalanceMutationResult{}, err
	}
	return BalanceMutationResult{Balance: BalanceView(row)}, nil
}

func (r *Loyalty) applyBalanceDebit(ctx context.Context, client *ent.Client, adjustment BalanceAdjustment) (BalanceMutationResult, error) {
	row, found, err := optionalRow(r.lockedAdjustmentBalance(ctx, client, adjustment))
	if err != nil || !found {
		return BalanceMutationResult{}, err
	}
	row, spent, err := spendRow(ctx, client, row.ID, Spend{
		UserID: adjustment.UserID, ViewerID: adjustment.ViewerID, ViewerLogin: adjustment.ViewerLogin, Amount: adjustment.Value,
	})
	if err != nil {
		return BalanceMutationResult{}, err
	}
	return BalanceMutationResult{Balance: BalanceView(row), Spent: spent}, nil
}

func (r *Loyalty) replayBalanceMutation(ctx context.Context, tx *sql.Tx, op balanceOperation) (BalanceMutationResult, error) {
	query := "SELECT payload_hash, account_created_at, balance_reply FROM loyalty_balance_credits WHERE user_id=? AND key_hash=?"
	if r.dialect != "sqlite3" {
		query += " FOR UPDATE"
	}
	return op.readReceipt(tx.QueryRowContext(ctx, query, op.UserID, op.keyHash[:]))
}

type balanceReceiptReply struct {
	*loyaltyrpc.Balance
	Spent bool `json:"spent,omitempty"`
}

func (op balanceOperation) readReceipt(row *sql.Row) (BalanceMutationResult, error) {
	var hash, body []byte
	var account int64
	err := row.Scan(&hash, &account, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return BalanceMutationResult{}, nil
	}
	if err != nil {
		return BalanceMutationResult{}, err
	}
	if !bytes.Equal(hash, op.payloadHash[:]) {
		return BalanceMutationResult{}, ErrCreditKeyMismatch
	}
	if account != op.AccountCreatedAt {
		return BalanceMutationResult{}, ErrCreditAccountMismatch
	}
	var reply balanceReceiptReply
	if err := codec.Unmarshal(body, &reply); err != nil {
		return BalanceMutationResult{}, err
	}
	return BalanceMutationResult{Balance: reply.Balance, Spent: reply.Spent, Replayed: true}, nil
}

func (op balanceOperation) insertReceipt(ctx context.Context, tx *sql.Tx, result BalanceMutationResult) error {
	body, err := codec.Marshal(balanceReceiptReply{Balance: result.Balance, Spent: result.Spent})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO loyalty_balance_credits
		(user_id, key_hash, payload_hash, account_created_at, balance_reply, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		op.UserID, op.keyHash[:], op.payloadHash[:], op.AccountCreatedAt, body, time.Now().UnixMilli())
	return err
}

func (a loyaltyAccountState) admitsInstance(instance int64) bool {
	return a.instance == 0 || a.instance == instance
}
