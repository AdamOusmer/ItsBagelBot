// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
)

func (r *Loyalty) EnsureWatchSchema(ctx context.Context) error {
	if err := r.ensureWatchTables(ctx); err != nil {
		return err
	}
	if err := r.ensureWatchOperationState(ctx); err != nil {
		return err
	}
	if err := r.ensureWatchCreatedIndexes(ctx); err != nil {
		return err
	}
	if err := r.ensureWatchWindowStartColumns(ctx); err != nil {
		return err
	}
	if err := r.ensureWatchRetentionSchema(ctx); err != nil {
		return err
	}
	return r.ensureFreezeTables(ctx)
}
func (r *Loyalty) insertIgnore() string {
	if r.dialect == "sqlite3" {
		return "INSERT OR IGNORE"
	}
	return "INSERT IGNORE"
}

const accountFenceQuery = "SELECT account_created_at,deleted FROM loyalty_account_fences WHERE user_id=?"

func (r *Loyalty) fence(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (loyaltyAccountState, error) {
	if r.dialect == "sqlite3" {
		return r.createAccountFence(ctx, tx, id, instance)
	}
	account, err := readAccountFence(tx.QueryRowContext(ctx, accountFenceQuery+" FOR UPDATE", id))
	if !errors.Is(err, sql.ErrNoRows) {
		return account, err
	}
	return r.createAccountFence(ctx, tx, id, 0)
}

func (r *Loyalty) createAccountFence(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (loyaltyAccountState, error) {
	stmt, lock := "INSERT INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES (?, ?, 0) ON DUPLICATE KEY UPDATE user_id=user_id", " FOR UPDATE"
	if r.dialect == "sqlite3" {
		stmt, lock = "INSERT OR IGNORE INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES (?, ?, 0)", ""
	}
	result, err := tx.ExecContext(ctx, stmt, id, instance)
	if err != nil {
		return loyaltyAccountState{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return loyaltyAccountState{}, err
	}
	account, err := readAccountFence(tx.QueryRowContext(ctx, accountFenceQuery+lock, id))
	account.created = inserted != 0
	return account, err
}

func readAccountFence(row *sql.Row) (loyaltyAccountState, error) {
	var account loyaltyAccountState
	var deleted int
	err := row.Scan(&account.instance, &deleted)
	account.deleted = deleted != 0
	return account, err
}

func (r *Loyalty) ApplyWatchAward(ctx context.Context, a data.WatchAwardDTO) error {
	if err := watchtime.ValidateAward(a); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	body, err := codec.Marshal(a)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	posting := watchPosting{award: a, postedAt: time.Now(), hash: hex.EncodeToString(digest[:])}
	return r.applyWatchAward(ctx, posting)
}

func (r *Loyalty) RestoreUser(ctx context.Context, id uint64, instance int64) error {
	if id == 0 || instance <= 0 {
		return nil
	}
	return r.changeAccount(ctx, id, func(ctx context.Context, tx *sql.Tx) (bool, error) {
		return r.restoreUser(ctx, tx, id, instance)
	})
}

func (r *Loyalty) restoreUser(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (bool, error) {
	account, err := r.fence(ctx, tx, id, instance)
	if err != nil {
		return false, err
	}
	if instance <= account.instance {
		return false, nil
	}
	if err := r.replaceLoyaltyAccount(ctx, tx, loyaltyAccountReplacement{userID: id, prior: account.instance, next: instance}); err != nil {
		return false, err
	}
	return !account.created, nil
}

func (r *Loyalty) DeleteAccount(ctx context.Context, id uint64, instance int64) error {
	if id == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	return r.changeAccount(ctx, id, func(ctx context.Context, tx *sql.Tx) (bool, error) {
		return r.retireLoyaltyAccount(ctx, tx, id, instance)
	})
}

func (r *Loyalty) changeAccount(ctx context.Context, id uint64, change func(context.Context, *sql.Tx) (bool, error)) error {
	var changed bool
	err := retryTx(ctx, func(ctx context.Context) error {
		tx, err := r.sqldb.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		changed, err = change(ctx, tx)
		if err != nil {
			return err
		}
		return tx.Commit()
	})
	if err == nil && changed {
		r.publishAccountChange(id)
	}
	return err
}

type watchHistoryTable struct {
	name     string
	identity string
}

var watchHistoryTables = []watchHistoryTable{
	{name: "loyalty_watch_operations", identity: "operation_id"},
	{name: "loyalty_watch_viewers", identity: "award_id"},
}

func (r *Loyalty) ensureWatchTables(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS loyalty_earned_receipts (batch_id VARCHAR(64) PRIMARY KEY, payload_hash VARCHAR(64) NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS loyalty_account_fences (user_id BIGINT PRIMARY KEY, account_created_at BIGINT NOT NULL, deleted INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS loyalty_watch_operations (operation_id VARCHAR(64) PRIMARY KEY, payload_hash VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL, window_started_at_ms BIGINT NOT NULL DEFAULT 0, completed INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS loyalty_watch_viewers (award_id VARCHAR(64) PRIMARY KEY, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL, window_started_at_ms BIGINT NOT NULL DEFAULT 0)`,
	} {
		if _, err := r.sqldb.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) ensureWatchCreatedIndexes(ctx context.Context) error {
	for _, table := range watchHistoryTables {
		if err := r.ensureWatchIndex(ctx, table, watchHistoryIndex{suffix: "created", columns: "created_at"}); err != nil {
			return err
		}
	}
	return nil
}

type watchAdmission struct {
	allowed  bool
	replaced bool
}

func (r *Loyalty) admitWatchAward(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO) (watchAdmission, error) {
	account, err := r.fence(ctx, tx, a.UserID, a.AccountCreatedAt)
	if err != nil {
		return watchAdmission{}, err
	}
	allowed, err := r.admitWatchAccount(ctx, tx, a, account)
	return watchAdmission{allowed: allowed, replaced: !account.created && a.AccountCreatedAt > account.instance}, err
}

func (r *Loyalty) watchWindowOpen(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO) (bool, error) {
	finalized, err := r.readWatchRetention(ctx, tx)
	if err != nil {
		return false, err
	}
	if finalized <= 0 {
		return true, nil
	}
	return watchtime.WindowStartUnixMilli(a) > finalized, nil
}

type loyaltyAccountState struct {
	instance int64
	deleted  bool
	created  bool
}

type loyaltyAccountReplacement struct {
	userID uint64
	prior  int64
	next   int64
}

func (r *Loyalty) admitWatchAccount(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, account loyaltyAccountState) (bool, error) {
	if a.AccountCreatedAt > account.instance {
		err := r.replaceLoyaltyAccount(ctx, tx, loyaltyAccountReplacement{userID: a.UserID, prior: account.instance, next: a.AccountCreatedAt})
		return err == nil, err
	}
	if account.deleted {
		return false, nil
	}
	return account.instance == a.AccountCreatedAt, nil
}

func (r *Loyalty) replaceLoyaltyAccount(ctx context.Context, tx *sql.Tx, replacement loyaltyAccountReplacement) error {
	if replacement.prior > 0 {
		if err := r.deleteLoyaltyRows(ctx, tx, replacement.userID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=0 WHERE user_id=?", replacement.next, replacement.userID)
	return err
}

func (r *Loyalty) deleteLoyaltyRows(ctx context.Context, tx *sql.Tx, id uint64) error {
	if _, err := r.createFreezeRow(ctx, tx, id); err != nil {
		return err
	}
	for _, table := range []string{"loyalty_freezes", "loyalty_snapshots", "loyalty_freeze_balances", "balances", "counter_entries", "counters"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id=?", id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) retireLoyaltyAccount(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (bool, error) {
	account, err := r.fence(ctx, tx, id, instance)
	if err != nil {
		return false, err
	}
	instance, allowed := retirementInstance(instance, account.instance)
	if !allowed {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=1 WHERE user_id=?", instance, id); err != nil {
		return false, err
	}
	err = r.deleteLoyaltyRows(ctx, tx, id)
	return err == nil, err
}

func retirementInstance(instance, current int64) (int64, bool) {
	if instance == 0 && current > 0 {
		return current, false
	}
	if instance > 0 && instance < current {
		return current, false
	}
	return max(instance, current), true
}
