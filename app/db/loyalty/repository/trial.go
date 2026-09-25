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
	ensureTrialRows     = "INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', 0, ?, ?), (?, ?, 'channel', 0, ?, ?) ON DUPLICATE KEY UPDATE value = value"
	lockTrialRow        = "SELECT value FROM counters WHERE user_id = ? AND name = ? FOR UPDATE"
	claimTrialPromotion = "INSERT IGNORE INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', 1, ?, ?)"
	addChannelCounter   = "INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', ?, ?, ?) ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)"
)

var errTrialTotalsMoved = errors.New("trial totals changed during promotion")

type trialTotals struct {
	decoded  int64
	answered int64
}

type trialCarry struct {
	name  string
	delta int64
}

func (t trialTotals) carried() []trialCarry {
	return []trialCarry{
		{data.CounterCommandsAnswered, t.answered},
		{data.CounterEventsProcessed, t.decoded},
		{data.CounterMessagesProcessed, t.decoded},
	}
}

// PromoteTrial carries a trial's decoded envelopes and answered commands into the
// channel counters once; the trial_promoted row makes every later call a no-op.
func (r *Loyalty) PromoteTrial(ctx context.Context, userID uint64) (bool, error) {
	promoted := false
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error {
			done, err := r.promoteTrialTx(ctx, userID)
			promoted = done
			return err
		}, errTrialTotalsMoved)
	})
	return promoted, err
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
	p := trialPromotion{tx: tx, userID: userID, now: time.Now()}
	if err := p.addTotals(ctx, seen); err != nil {
		return false, err
	}
	if err := p.verifyTotals(ctx, seen); err != nil {
		return false, err
	}
	claimed, err := p.claim(ctx)
	if err != nil || !claimed {
		return false, err
	}
	return true, tx.Commit()
}

type trialPromotion struct {
	tx     *sql.Tx
	userID uint64
	now    time.Time
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

func (p trialPromotion) addTotals(ctx context.Context, totals trialTotals) error {
	for _, add := range totals.carried() {
		if add.delta <= 0 {
			continue
		}
		if _, err := p.tx.ExecContext(ctx, addChannelCounter, p.userID, add.name, add.delta, p.now, p.now); err != nil {
			return err
		}
	}
	return nil
}

// READ COMMITTED locks nothing for an absent row, so the trial rows are created before they are locked.
func (p trialPromotion) verifyTotals(ctx context.Context, seen trialTotals) error {
	if _, err := p.tx.ExecContext(ctx, ensureTrialRows, p.userID, data.CounterTrialAnswered, p.now, p.now, p.userID, data.CounterTrialDecoded, p.now, p.now); err != nil {
		return err
	}
	answered, err := p.lockCounter(ctx, data.CounterTrialAnswered)
	if err != nil {
		return err
	}
	decoded, err := p.lockCounter(ctx, data.CounterTrialDecoded)
	if err != nil {
		return err
	}
	if (trialTotals{decoded: decoded, answered: answered}) != seen {
		return errTrialTotalsMoved
	}
	return nil
}

func (p trialPromotion) lockCounter(ctx context.Context, name string) (int64, error) {
	var value int64
	err := p.tx.QueryRowContext(ctx, lockTrialRow, p.userID, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return value, err
}

func (p trialPromotion) claim(ctx context.Context) (bool, error) {
	claimed, err := p.tx.ExecContext(ctx, claimTrialPromotion, p.userID, data.CounterTrialPromoted, p.now, p.now)
	if err != nil {
		return false, err
	}
	n, err := claimed.RowsAffected()
	return n != 0, err
}
