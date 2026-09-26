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
	"math"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/codec"
	"github.com/go-sql-driver/mysql"
)

// EnsureWatchSchema installs the narrow durable inbox and lifecycle fences.
// Called at service startup before accepting any events or outbox deliveries.
func (r *Loyalty) EnsureWatchSchema(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS loyalty_account_fences (user_id BIGINT PRIMARY KEY, account_created_at BIGINT NOT NULL, deleted INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS loyalty_watch_operations (operation_id VARCHAR(64) PRIMARY KEY, payload_hash VARCHAR(64) NOT NULL, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL, window_started_at_ms BIGINT NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS loyalty_watch_viewers (award_id VARCHAR(64) PRIMARY KEY, user_id BIGINT NOT NULL, created_at BIGINT NOT NULL, window_started_at_ms BIGINT NOT NULL DEFAULT 0)`,
	} {
		if _, err := r.sqldb.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		name := "idx_" + table + "_created"
		if r.dialect == "sqlite3" {
			if _, err := r.sqldb.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS "+name+" ON "+table+" (created_at)"); err != nil {
				return err
			}
			continue
		}
		var present int
		if err := r.sqldb.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?", table, name).Scan(&present); err != nil {
			return err
		}
		if present == 0 {
			if _, err := r.sqldb.ExecContext(ctx, "CREATE INDEX "+name+" ON "+table+" (created_at)"); err != nil {
				var myErr *mysql.MySQLError
				if !errors.As(err, &myErr) || myErr.Number != 1061 {
					return err
				}
			}
		}
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
	hash := hex.EncodeToString(digest[:])
	op := watchtime.OperationID(a)
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	finalized, err := r.readWatchRetention(ctx, tx)
	if err != nil {
		return err
	}
	windowStart := watchtime.WindowStartUnixMilli(a)
	if finalized > 0 && windowStart <= finalized {
		return tx.Commit()
	}
	current, deleted, err := r.fence(ctx, tx, a.UserID, a.AccountCreatedAt)
	if err != nil {
		return err
	}
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT payload_hash FROM loyalty_watch_operations WHERE operation_id=?", op).Scan(&previous)
	if err == nil {
		if previous != hash {
			return errors.New("watch operation payload mismatch")
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO loyalty_watch_operations (operation_id,payload_hash,user_id,created_at,window_started_at_ms) VALUES (?, ?, ?, ?, ?)", op, hash, a.UserID, time.Now().Unix(), windowStart); err != nil {
		return err
	}
	// Admission was accepted against the source incarnation. Its award may
	// outrun UserChanged on the independent lifecycle subscription.
	if a.AccountCreatedAt > current {
		if current > 0 {
			for _, table := range []string{"balances", "counter_entries", "counters"} {
				if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id=?", a.UserID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=0 WHERE user_id=?", a.AccountCreatedAt, a.UserID); err != nil {
			return err
		}
		current = a.AccountCreatedAt
		deleted = false
	}
	if deleted || current != a.AccountCreatedAt {
		return tx.Commit()
	}
	now := time.Now()
	for _, e := range a.Entries {
		entryHash := sha256.Sum256([]byte(strconv.FormatUint(a.UserID, 10) + ":" + strconv.FormatInt(a.AccountCreatedAt, 10) + ":" + a.LiveSession + ":" + a.WindowID + ":" + strconv.FormatUint(e.ViewerID, 10)))
		result, err := tx.ExecContext(ctx, r.insertIgnore()+" INTO loyalty_watch_viewers (award_id,user_id,created_at,window_started_at_ms) VALUES (?, ?, ?, ?)", hex.EncodeToString(entryHash[:]), a.UserID, time.Now().Unix(), windowStart)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		var existingPoints int64
		var existingWatch uint64
		readErr := tx.QueryRowContext(ctx, "SELECT points,watch_seconds FROM balances WHERE user_id=? AND viewer_id=?", a.UserID, e.ViewerID).Scan(&existingPoints, &existingWatch)
		if readErr != nil && !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}
		if existingPoints > math.MaxInt64-e.Points || existingWatch > math.MaxUint64-e.WatchSeconds {
			return fmt.Errorf("%w: watch balance overflow", ErrInvalidInput)
		}
		stmt := `INSERT INTO balances (user_id,viewer_id,viewer_login,viewer_name,points,watch_seconds,created_at,updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		if r.dialect == "sqlite3" {
			stmt += ` ON CONFLICT(user_id,viewer_id) DO UPDATE SET points=points+excluded.points, watch_seconds=watch_seconds+excluded.watch_seconds, viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END, viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END, updated_at=excluded.updated_at`
		} else {
			stmt += ` ON DUPLICATE KEY UPDATE points=points+VALUES(points),watch_seconds=watch_seconds+VALUES(watch_seconds),viewer_login=IF(VALUES(viewer_login)='',viewer_login,VALUES(viewer_login)),viewer_name=IF(VALUES(viewer_name)='',viewer_name,VALUES(viewer_name)),updated_at=VALUES(updated_at)`
		}
		if _, err := tx.ExecContext(ctx, stmt, a.UserID, e.ViewerID, e.ViewerLogin, e.ViewerName, e.Points, e.WatchSeconds, now, now); err != nil {
			return err
		}
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
		if current > 0 {
			for _, table := range []string{"balances", "counter_entries", "counters"} {
				if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id=?", id); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=0 WHERE user_id=?", instance, id); err != nil {
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
	current, _, err := r.fence(ctx, tx, id, instance)
	if err != nil {
		return err
	}
	// A legacy event cannot prove which incarnation it retires.
	if instance == 0 && current > 0 {
		return tx.Commit()
	}
	if instance > 0 && instance < current {
		return tx.Commit()
	}
	if instance < current {
		instance = current
	}
	if _, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET account_created_at=?,deleted=1 WHERE user_id=?", instance, id); err != nil {
		return err
	}
	for _, table := range []string{"balances", "counter_entries", "counters"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE user_id=?", id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.earnPend {
		if key.userID == id {
			delete(r.earnPend, key)
		}
	}
	for key := range r.bumpPend {
		if key.userID == id {
			delete(r.bumpPend, key)
		}
	}
	return nil
}
