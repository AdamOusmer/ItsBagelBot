// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

const (
	claimTrialPromotion = "INSERT IGNORE INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', 1, ?, ?)"
	readTrialTotals     = "SELECT COALESCE(SUM(CASE WHEN name = ? THEN value END), 0), COALESCE(SUM(CASE WHEN name = ? THEN value END), 0) FROM counters WHERE user_id = ?"
	addChannelCounter   = "INSERT INTO counters (user_id, name, scope, value, created_at, updated_at) VALUES (?, ?, 'channel', ?, ?, ?) ON DUPLICATE KEY UPDATE value = value + VALUES(value), updated_at = VALUES(updated_at)"
)

// PromoteTrial carries a trial's decoded envelopes and answered commands into the
// channel counters once; the trial_promoted row makes every later call a no-op.
func (r *Loyalty) PromoteTrial(ctx context.Context, userID uint64) (bool, error) {
	promoted := false
	err := db.WithExec(ctx, func(ctx context.Context) error {
		done, err := r.promoteTrialTx(ctx, userID)
		promoted = done
		return err
	})
	return promoted, err
}

func (r *Loyalty) promoteTrialTx(ctx context.Context, userID uint64) (bool, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()
	claimed, err := tx.ExecContext(ctx, claimTrialPromotion, userID, data.CounterTrialPromoted, now, now)
	if err != nil {
		return false, err
	}
	if n, _ := claimed.RowsAffected(); n == 0 {
		return false, nil
	}
	if err := carryTrialTotals(ctx, tx, userID, now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func carryTrialTotals(ctx context.Context, tx *sql.Tx, userID uint64, now time.Time) error {
	var decoded, answered int64
	if err := tx.QueryRowContext(ctx, readTrialTotals, data.CounterTrialDecoded, data.CounterTrialAnswered, userID).Scan(&decoded, &answered); err != nil {
		return err
	}
	for _, add := range []struct {
		name  string
		delta int64
	}{
		{data.CounterEventsProcessed, decoded},
		{data.CounterMessagesProcessed, decoded},
		{data.CounterCommandsAnswered, answered},
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
