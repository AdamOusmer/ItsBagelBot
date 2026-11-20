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

type BalanceCredit struct {
	UserID, ViewerID uint64
	ViewerLogin      string
	Value            int64
	Key              string
	AccountCreatedAt int64
}

type CreditResult struct {
	Balance  *loyaltyrpc.Balance
	Replayed bool
}

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

func (c BalanceCredit) validate() error {
	if c.UserID == 0 || c.ViewerID == 0 {
		return fmt.Errorf("%w: user_id/viewer_id", ErrInvalidInput)
	}
	if c.Value <= 0 || c.AccountCreatedAt < 0 {
		return fmt.Errorf("%w: value/account_created_at", ErrInvalidInput)
	}
	if !creditKeyPrefixed(c.Key) {
		return fmt.Errorf("%w: key requires a caller prefix and value", ErrInvalidInput)
	}
	if len(validate.NormalizeLogin(c.ViewerLogin)) > maxCounterName {
		return fmt.Errorf("%w: viewer_login", ErrInvalidInput)
	}
	return nil
}

func creditKeyPrefixed(key string) bool {
	prefix, value, _ := strings.Cut(key, ":")
	return strings.TrimSpace(prefix) != "" && strings.TrimSpace(value) != ""
}

type creditOperation struct {
	BalanceCredit
	keyHash, payloadHash [sha256.Size]byte
}

func (c BalanceCredit) payloadHash() [sha256.Size]byte {
	var body [24]byte
	binary.BigEndian.PutUint64(body[0:8], c.ViewerID)
	binary.BigEndian.PutUint64(body[8:16], uint64(c.Value))
	binary.BigEndian.PutUint64(body[16:24], uint64(c.AccountCreatedAt))
	return sha256.Sum256(body[:])
}

func (r *Loyalty) BalanceCredit(ctx context.Context, credit BalanceCredit) (CreditResult, error) {
	if err := credit.validate(); err != nil {
		return CreditResult{}, err
	}
	op := creditOperation{BalanceCredit: credit, keyHash: sha256.Sum256([]byte(credit.Key)), payloadHash: credit.payloadHash()}
	result, err := r.commitCredit(ctx, op)
	if err != nil {
		return result, err
	}
	r.publishBalanceChange(credit.UserID, credit.ViewerID)
	return result, nil
}

func (r *Loyalty) commitCredit(ctx context.Context, op creditOperation) (CreditResult, error) {
	var result CreditResult
	err := retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			var err error
			result, err = r.creditTransaction(ctx, op)
			return err
		})
	})
	if err != nil {
		return CreditResult{}, err
	}
	return result, nil
}

func (r *Loyalty) creditTransaction(ctx context.Context, op creditOperation) (CreditResult, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return CreditResult{}, err
	}
	defer tx.Rollback()
	result, err := r.applyCredit(ctx, tx, op)
	if err != nil {
		return CreditResult{}, err
	}
	return result, tx.Commit()
}

func (r *Loyalty) applyCredit(ctx context.Context, tx *sql.Tx, op creditOperation) (CreditResult, error) {
	account, err := r.fence(ctx, tx, op.UserID, 0)
	if err != nil {
		return CreditResult{}, err
	}
	if account.deleted {
		return CreditResult{}, ErrCreditAccountRetired
	}
	if !account.admitsInstance(op.AccountCreatedAt) {
		return CreditResult{}, ErrCreditAccountMismatch
	}
	receipt := creditReceipt{op: op, account: op.AccountCreatedAt}
	result, err := r.replayCredit(ctx, tx, receipt)
	if err != nil || result.Replayed {
		return result, err
	}
	client := ent.NewClient(ent.Driver(db.EntTransactionDriver(r.dialect, tx)))
	adjustment := BalanceAdjustment{UserID: op.UserID, ViewerID: op.ViewerID, ViewerLogin: validate.NormalizeLogin(op.ViewerLogin), Value: op.Value}
	row, update, err := r.prepareBalanceAdjustment(ctx, client, adjustment)
	if err != nil {
		return CreditResult{}, err
	}
	row, err = update.SetViewerLogin(displayUpdate(row.ViewerLogin, adjustment.ViewerLogin)).Save(ctx)
	if err != nil {
		return CreditResult{}, err
	}
	reply := BalanceView(row)
	return CreditResult{Balance: reply}, receipt.insert(ctx, tx, reply)
}

type creditReceipt struct {
	op      creditOperation
	account int64
}

func (r *Loyalty) replayCredit(ctx context.Context, tx *sql.Tx, receipt creditReceipt) (CreditResult, error) {
	query := "SELECT payload_hash, account_created_at, balance_reply FROM loyalty_balance_credits WHERE user_id=? AND key_hash=?"
	if r.dialect != "sqlite3" {
		query += " FOR UPDATE"
	}
	return receipt.replay(tx.QueryRowContext(ctx, query, receipt.op.UserID, receipt.op.keyHash[:]))
}

func (c creditReceipt) replay(row *sql.Row) (CreditResult, error) {
	var hash, body []byte
	var account int64
	err := row.Scan(&hash, &account, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return CreditResult{}, nil
	}
	if err != nil {
		return CreditResult{}, err
	}
	if !bytes.Equal(hash, c.op.payloadHash[:]) {
		return CreditResult{}, ErrCreditKeyMismatch
	}
	if account != c.account {
		return CreditResult{}, ErrCreditAccountMismatch
	}
	var balance loyaltyrpc.Balance
	if err := codec.Unmarshal(body, &balance); err != nil {
		return CreditResult{}, err
	}
	return CreditResult{Balance: &balance, Replayed: true}, nil
}

func (c creditReceipt) insert(ctx context.Context, tx *sql.Tx, reply *loyaltyrpc.Balance) error {
	body, err := codec.Marshal(reply)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO loyalty_balance_credits
		(user_id, key_hash, payload_hash, account_created_at, balance_reply, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.op.UserID, c.op.keyHash[:], c.op.payloadHash[:], c.account, body, time.Now().UnixMilli())
	return err
}

func (a loyaltyAccountState) admitsInstance(instance int64) bool {
	return a.instance == 0 || a.instance == instance
}
