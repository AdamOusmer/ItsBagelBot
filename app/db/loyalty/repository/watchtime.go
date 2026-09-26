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
	"maps"
	"math"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
)

// EnsureWatchSchema installs the narrow durable inbox and lifecycle fences.
// Called at service startup before accepting any events or outbox deliveries.
func (r *Loyalty) EnsureWatchSchema(ctx context.Context) error {
	if err := r.ensureWatchTables(ctx); err != nil {
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
	r.watchReady.Store(true)
	return nil
}
func (r *Loyalty) insertIgnore() string {
	if r.dialect == "sqlite3" {
		return "INSERT OR IGNORE"
	}
	return "INSERT IGNORE"
}
func (r *Loyalty) fence(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (int64, bool, error) {
	stmt := r.insertIgnore() + " INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES (?, ?, 0)"
	if r.dialect != "sqlite3" {
		// Duplicate-key UPDATE takes an exclusive lock immediately. INSERT
		// IGNORE followed by UPDATE can deadlock concurrent duplicate awards
		// as both transactions upgrade shared duplicate-key locks.
		stmt = "INSERT INTO loyalty_account_fences (user_id,account_created_at,deleted) VALUES (?, ?, 0) ON DUPLICATE KEY UPDATE user_id=user_id"
	}
	if _, err := tx.ExecContext(ctx, stmt, id, instance); err != nil {
		return 0, false, err
	}
	// An UPDATE takes an exclusive row lock on MySQL, serializing retirement,
	// recreation and posting. SQLite serializes its writer transactions.
	if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET deleted=deleted WHERE user_id=?", id); err != nil {
		return 0, false, err
	}
	var current int64
	var deleted int
	err := tx.QueryRowContext(ctx, "SELECT account_created_at,deleted FROM loyalty_account_fences WHERE user_id=?", id).Scan(&current, &deleted)
	return current, deleted != 0, err
}

// ApplyWatchAward returns only after the inbox, per-window viewer dedup and
// balance credits commit together. Retired account instances are consumed
// without resurrecting rows; errors leave the outbox delivery pending.
func (r *Loyalty) ApplyWatchAward(ctx context.Context, a data.WatchAwardDTO) error {
	if err := watchtime.ValidateAward(a); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	body, err := codec.Marshal(a)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(body)
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.applyWatchAward(ctx, tx, a, hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	return tx.Commit()
}

// RestoreUser uses the source account creation timestamp, never event arrival
// order, to distinguish a genuine recreation from delayed previous-instance work.
func (r *Loyalty) RestoreUser(ctx context.Context, id uint64, instance int64) error {
	if id == 0 || instance <= 0 {
		return nil
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, _, err := r.fence(ctx, tx, id, instance)
	if err != nil {
		return err
	}
	if instance > current {
		if err := replaceLoyaltyAccount(ctx, tx, loyaltyAccountReplacement{userID: id, prior: current, next: instance}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Loyalty) DeleteAccount(ctx context.Context, id uint64, instance int64) error {
	if id == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	retired, err := r.retireLoyaltyAccount(ctx, tx, id, instance)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if retired {
		r.discardPendingAccount(id)
	}
	return nil
}

// watchHistoryTable identifies a replay ledger and its deduplication key.
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
		`CREATE TABLE IF NOT EXISTS loyalty_account_fences (user_id BIGINT PRIMARY KEY, account_created_at BIGINT NOT NULL, deleted INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS loyalty_watch_operations (operation_id VARCHAR(64) PRIMARY KEY, payload_hash VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL, window_started_at_ms BIGINT NOT NULL DEFAULT 0)`,
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

// Lock order remains retention, account, operation, viewer, then balance.
func (r *Loyalty) applyWatchAward(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, hash string) error {
	allowed, err := r.recordAdmittedWatchAward(ctx, tx, a, hash)
	if err != nil || !allowed {
		return err
	}
	return r.postWatchViewers(ctx, tx, a)
}

func (r *Loyalty) recordAdmittedWatchAward(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, hash string) (bool, error) {
	open, err := r.watchWindowOpen(ctx, tx, a)
	if err != nil || !open {
		return false, err
	}
	current, deleted, err := r.fence(ctx, tx, a.UserID, a.AccountCreatedAt)
	if err != nil {
		return false, err
	}
	added, err := recordWatchOperation(ctx, tx, a, hash)
	if err != nil || !added {
		return false, err
	}
	return admitWatchAccount(ctx, tx, a, loyaltyAccountState{instance: current, deleted: deleted})
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

func recordWatchOperation(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, hash string) (bool, error) {
	op := watchtime.OperationID(a)
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT payload_hash FROM loyalty_watch_operations WHERE operation_id=?", op).Scan(&previous)
	if err == nil {
		if previous != hash {
			return false, errors.New("watch operation payload mismatch")
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO loyalty_watch_operations (operation_id,payload_hash,user_id,created_at,window_started_at_ms) VALUES (?, ?, ?, ?, ?)", op, hash, a.UserID, time.Now().Unix(), watchtime.WindowStartUnixMilli(a))
	return true, err
}

// loyaltyAccountState is the lifecycle fence read under the account row lock.
type loyaltyAccountState struct {
	instance int64
	deleted  bool
}

type loyaltyAccountReplacement struct {
	userID uint64
	prior  int64
	next   int64
}

// Accepted awards may outrun the independent UserChanged subscription.
func admitWatchAccount(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, account loyaltyAccountState) (bool, error) {
	if a.AccountCreatedAt > account.instance {
		err := replaceLoyaltyAccount(ctx, tx, loyaltyAccountReplacement{userID: a.UserID, prior: account.instance, next: a.AccountCreatedAt})
		return err == nil, err
	}
	if account.deleted {
		return false, nil
	}
	return account.instance == a.AccountCreatedAt, nil
}

func replaceLoyaltyAccount(ctx context.Context, tx *sql.Tx, replacement loyaltyAccountReplacement) error {
	if replacement.prior > 0 {
		if err := deleteLoyaltyRows(ctx, tx, replacement.userID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=0 WHERE user_id=?", replacement.next, replacement.userID)
	return err
}

func deleteLoyaltyRows(ctx context.Context, tx *sql.Tx, id uint64) error {
	for _, table := range []string{"balances", "counter_entries", "counters"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id=?", id); err != nil {
			return err
		}
	}
	return nil
}

// watchPosting shares the accepted window and posting timestamp across viewers.
type watchPosting struct {
	award    data.WatchAwardDTO
	postedAt time.Time
}

func (r *Loyalty) postWatchViewers(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO) error {
	posting := watchPosting{award: a, postedAt: time.Now()}
	for _, e := range a.Entries {
		if err := r.postWatchViewer(ctx, tx, posting, e); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) postWatchViewer(ctx context.Context, tx *sql.Tx, posting watchPosting, e data.LoyaltyEarnEntry) error {
	a := posting.award
	added, err := r.recordWatchViewer(ctx, tx, a, e.ViewerID)
	if err != nil || !added {
		return err
	}
	if err := checkWatchBalance(ctx, tx, a.UserID, e); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, r.watchBalanceUpsert(), a.UserID, e.ViewerID, e.ViewerLogin, e.ViewerName, e.Points, e.WatchSeconds, posting.postedAt, posting.postedAt)
	return err
}

func (r *Loyalty) recordWatchViewer(ctx context.Context, tx *sql.Tx, a data.WatchAwardDTO, viewer uint64) (bool, error) {
	identity := strconv.FormatUint(a.UserID, 10) + ":" + strconv.FormatInt(a.AccountCreatedAt, 10) + ":" + a.LiveSession + ":" + a.WindowID + ":" + strconv.FormatUint(viewer, 10)
	digest := sha256.Sum256([]byte(identity))
	result, err := tx.ExecContext(ctx, r.insertIgnore()+" INTO loyalty_watch_viewers (award_id,user_id,created_at,window_started_at_ms) VALUES (?, ?, ?, ?)", hex.EncodeToString(digest[:]), a.UserID, time.Now().Unix(), watchtime.WindowStartUnixMilli(a))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n != 0, err
}

func checkWatchBalance(ctx context.Context, tx *sql.Tx, user uint64, e data.LoyaltyEarnEntry) error {
	var points int64
	var seconds uint64
	err := tx.QueryRowContext(ctx, "SELECT points,watch_seconds FROM balances WHERE user_id=? AND viewer_id=?", user, e.ViewerID).Scan(&points, &seconds)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return err
	}
	if !watchBalanceFits(points, seconds, e) {
		return fmt.Errorf("%w: watch balance overflow", ErrInvalidInput)
	}
	return nil
}

func watchBalanceFits(points int64, seconds uint64, e data.LoyaltyEarnEntry) bool {
	if points > math.MaxInt64-e.Points {
		return false
	}
	return seconds <= math.MaxUint64-e.WatchSeconds
}

func (r *Loyalty) watchBalanceUpsert() string {
	stmt := `INSERT INTO balances (user_id,viewer_id,viewer_login,viewer_name,points,watch_seconds,created_at,updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	if r.dialect == "sqlite3" {
		return stmt + ` ON CONFLICT(user_id,viewer_id) DO UPDATE SET points=points+excluded.points, watch_seconds=watch_seconds+excluded.watch_seconds, viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END, viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END, updated_at=excluded.updated_at`
	}
	return stmt + ` ON DUPLICATE KEY UPDATE points=points+VALUES(points),watch_seconds=watch_seconds+VALUES(watch_seconds),viewer_login=IF(VALUES(viewer_login)='',viewer_login,VALUES(viewer_login)),viewer_name=IF(VALUES(viewer_name)='',viewer_name,VALUES(viewer_name)),updated_at=VALUES(updated_at)`
}

func (r *Loyalty) retireLoyaltyAccount(ctx context.Context, tx *sql.Tx, id uint64, instance int64) (bool, error) {
	current, _, err := r.fence(ctx, tx, id, instance)
	if err != nil {
		return false, err
	}
	instance, allowed := retirementInstance(instance, current)
	if !allowed {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=1 WHERE user_id=?", instance, id); err != nil {
		return false, err
	}
	err = deleteLoyaltyRows(ctx, tx, id)
	return err == nil, err
}

func (r *Loyalty) discardPendingAccount(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	maps.DeleteFunc(r.earnPend, func(key balKey, _ *earnSum) bool { return key.userID == id })
	maps.DeleteFunc(r.bumpPend, func(key bumpKey, _ *bumpSum) bool { return key.userID == id })
}

// Unknown lineage cannot retire a known incarnation; stamped older deletions
// are likewise ignored. Preserve the existing legacy fence normalization.
func retirementInstance(instance, current int64) (int64, bool) {
	if instance == 0 && current > 0 {
		return current, false
	}
	if instance > 0 && instance < current {
		return current, false
	}
	return max(instance, current), true
}
