// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"strings"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
	"github.com/newrelic/go-agent/v3/newrelic"
)

func (r *Loyalty) persistCounterBatch(ctx context.Context, events []data.CounterBumpedDTO) error {
	txn := r.app.StartTransaction("persist counter batch")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error { return r.commitCounterEvents(ctx, events) }, errTrialPromotedMidBatch)
	})
	if err != nil {
		txn.NoticeError(err)
	}
	return err
}

func (r *Loyalty) commitCounterEvents(ctx context.Context, events []data.CounterBumpedDTO) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	fences, err := r.lockCounterUsers(ctx, tx, counterEventUserIDs(events))
	if err != nil {
		return err
	}
	events, err = r.admitCounterEvents(ctx, tx, events)
	if err != nil {
		return err
	}
	return r.commitAdmittedCounters(ctx, tx, events, fences)
}

func (r *Loyalty) commitAdmittedCounters(ctx context.Context, tx *sql.Tx, events []data.CounterBumpedDTO, fences map[uint64]counterFence) error {
	windows, err := collectCounterWindows(events, fences)
	if err != nil {
		return err
	}
	bumps, unpromoted, err := prepareCounterWindows(windows, fences)
	if err != nil {
		return err
	}
	if err := r.writeCounterSums(ctx, tx, bumps); err != nil {
		return err
	}
	if err := verifyTrialPromotions(ctx, tx, unpromoted); err != nil {
		return err
	}
	return tx.Commit()
}

func counterEventUserIDs(events []data.CounterBumpedDTO) []uint64 {
	users := make(map[uint64]bool)
	for _, event := range events {
		users[event.UserID] = true
	}
	return slices.Sorted(maps.Keys(users))
}

func collectCounterWindows(events []data.CounterBumpedDTO, fences map[uint64]counterFence) (map[uint64]*Loyalty, error) {
	windows := make(map[uint64]*Loyalty)
	for _, event := range events {
		if fences[event.UserID].retired {
			continue
		}
		if err := collectCounterEvent(counterWindow(windows, event.UserID), event); err != nil {
			return nil, err
		}
	}
	return windows, nil
}

func counterWindow(windows map[uint64]*Loyalty, id uint64) *Loyalty {
	if window := windows[id]; window != nil {
		return window
	}
	window := &Loyalty{bumpPend: make(map[bumpKey]*bumpSum)}
	windows[id] = window
	return window
}

func collectCounterEvent(window *Loyalty, event data.CounterBumpedDTO) error {
	for _, bump := range event.Bumps {
		if err := window.collectBatchBump(event.UserID, bump); err != nil {
			return err
		}
	}
	return nil
}

func prepareCounterWindows(windows map[uint64]*Loyalty, fences map[uint64]counterFence) (map[bumpKey]*bumpSum, []uint64, error) {
	bumps := make(map[bumpKey]*bumpSum)
	var unpromoted []uint64
	for _, id := range slices.Sorted(maps.Keys(windows)) {
		window := windows[id]
		needsRecheck, err := redirectCounterTrial(id, window, fences[id].promoted)
		if err != nil {
			return nil, nil, err
		}
		if needsRecheck {
			unpromoted = append(unpromoted, id)
		}
		maps.Copy(bumps, window.bumpPend)
	}
	return bumps, unpromoted, nil
}

func redirectCounterTrial(id uint64, window *Loyalty, promoted bool) (bool, error) {
	trial := batchTrialBumps(id, window.bumpPend)
	if trial == (trialTotals{}) {
		return false, nil
	}
	if !promoted {
		return true, nil
	}
	return false, collectTrialCarry(window, id, trial)
}

func collectTrialCarry(window *Loyalty, id uint64, trial trialTotals) error {
	for _, carry := range trial.carried() {
		bump := data.CounterBumpEntry{Name: carry.name, Scope: data.CounterScopeChannel, Delta: carry.delta}
		if err := window.collectBatchBump(id, bump); err != nil {
			return err
		}
	}
	return nil
}

func verifyTrialPromotions(ctx context.Context, tx *sql.Tx, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	query := "SELECT user_id FROM counters WHERE name='" + data.CounterTrialPromoted +
		"' AND user_id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errTrialPromotedMidBatch
	}
	return rows.Err()
}
