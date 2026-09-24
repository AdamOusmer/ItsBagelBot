// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

const (
	readTrialSnapshot   = "SELECT name, value FROM counters WHERE user_id = ? AND name IN (?, ?, ?)"
	lockTrialRow        = "SELECT value FROM counters WHERE user_id = ? AND name = ? FOR UPDATE"
	claimTrialPromotion = "INSERT IGNORE INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', 1, ?, ?)"
	addChannelCounter   = "INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', ?, ?, ?) ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)"
)

const promoteTrialAttempts = 3

var errTrialTotalsMoved = errors.New("trial totals changed during promotion")

type trialTotals struct {
	decoded  int64
	answered int64
}

// PromoteTrial carries a trial's decoded envelopes and answered commands into the
// channel counters once; the trial_promoted row makes every later call a no-op.
func (r *Loyalty) PromoteTrial(ctx context.Context, userID uint64) (bool, error) {
	promoted := false
	err := db.WithExec(ctx, func(ctx context.Context) error {
		done, err := r.promoteTrialRetrying(ctx, userID)
		promoted = done
		return err
	})
	return promoted, err
}

func (r *Loyalty) promoteTrialRetrying(ctx context.Context, userID uint64) (bool, error) {
	err := errTrialTotalsMoved
	for range promoteTrialAttempts {
		var done bool
		done, err = r.promoteTrialTx(ctx, userID)
		if !errors.Is(err, errTrialTotalsMoved) {
			return done, err
		}
	}
	return false, err
}

// Counter rows lock in ascending name order like flushBumps; reordering these steps can deadlock.
func (r *Loyalty) promoteTrialTx(ctx context.Context, userID uint64) (bool, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	seen, promoted, err := readTrialTotals(ctx, tx, userID)
	if err != nil || promoted {
		return false, err
	}
	now := time.Now()
	if err := addTrialTotals(ctx, tx, userID, seen, now); err != nil {
		return false, err
	}
	if err := verifyTrialTotals(ctx, tx, userID, seen); err != nil {
		return false, err
	}
	claimed, err := claimTrial(ctx, tx, userID, now)
	if err != nil || !claimed {
		return false, err
	}
	return true, tx.Commit()
}

func readTrialTotals(ctx context.Context, tx *sql.Tx, userID uint64) (trialTotals, bool, error) {
	rows, err := tx.QueryContext(ctx, readTrialSnapshot, userID, data.CounterTrialAnswered, data.CounterTrialDecoded, data.CounterTrialPromoted)
	if err != nil {
		return trialTotals{}, false, err
	}
	defer func() { _ = rows.Close() }()
	var totals trialTotals
	promoted := false
	for rows.Next() {
		var name string
		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return trialTotals{}, false, err
		}
		switch name {
		case data.CounterTrialAnswered:
			totals.answered = value
		case data.CounterTrialDecoded:
			totals.decoded = value
		case data.CounterTrialPromoted:
			promoted = true
		}
	}
	return totals, promoted, rows.Err()
}

func addTrialTotals(ctx context.Context, tx *sql.Tx, userID uint64, totals trialTotals, now time.Time) error {
	for _, add := range []struct {
		name  string
		delta int64
	}{
		{data.CounterCommandsAnswered, totals.answered},
		{data.CounterEventsProcessed, totals.decoded},
		{data.CounterMessagesProcessed, totals.decoded},
	} {
		if add.delta <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, addChannelCounter, userID, add.name, add.delta, now, now); err != nil {
			return err
		}
	}
	return nil
}

func verifyTrialTotals(ctx context.Context, tx *sql.Tx, userID uint64, seen trialTotals) error {
	answered, err := lockTrialCounter(ctx, tx, userID, data.CounterTrialAnswered)
	if err != nil {
		return err
	}
	decoded, err := lockTrialCounter(ctx, tx, userID, data.CounterTrialDecoded)
	if err != nil {
		return err
	}
	if (trialTotals{decoded: decoded, answered: answered}) != seen {
		return errTrialTotalsMoved
	}
	return nil
}

func lockTrialCounter(ctx context.Context, tx *sql.Tx, userID uint64, name string) (int64, error) {
	var value int64
	err := tx.QueryRowContext(ctx, lockTrialRow, userID, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return value, err
}

func claimTrial(ctx context.Context, tx *sql.Tx, userID uint64, now time.Time) (bool, error) {
	claimed, err := tx.ExecContext(ctx, claimTrialPromotion, userID, data.CounterTrialPromoted, now, now)
	if err != nil {
		return false, err
	}
	n, err := claimed.RowsAffected()
	return n != 0, err
}
