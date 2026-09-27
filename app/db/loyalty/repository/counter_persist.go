// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"maps"
	"sort"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
	"github.com/newrelic/go-agent/v3/newrelic"
)

// persistCounterBatch runs while persistMu is held. Every SQL error rolls back
// the complete coalesced transaction. Like the durable legacy writer, a trial
// promotion detected after writes restarts the rolled-back transaction with
// redirected totals. Only deadlocks and proven rolled-back promotion races
// are retried here; uncertain commit outcomes return to the handler.
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
	ids := counterEventUserIDs(events)
	retired, err := r.lockCounterUsers(ctx, tx, ids)
	if err != nil {
		return err
	}
	windows, err := collectCounterWindows(events, retired)
	if err != nil {
		return err
	}
	bumps, unpromoted, err := prepareCounterWindows(ctx, tx, ids, windows)
	if err != nil {
		return err
	}
	if err := r.writeCounterTransaction(ctx, tx, bumps); err != nil {
		return err
	}
	if err := r.verifyTrialPromotions(ctx, tx, unpromoted); err != nil {
		return err
	}
	return tx.Commit()
}

func counterEventUserIDs(events []data.CounterBumpedDTO) []uint64 {
	users := make(map[uint64]bool)
	for _, event := range events {
		users[event.UserID] = true
	}
	ids := make([]uint64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (r *Loyalty) lockCounterUsers(ctx context.Context, tx *sql.Tx, ids []uint64) (map[uint64]bool, error) {
	retired := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		deleted, err := r.counterBatchRetired(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		retired[id] = deleted
	}
	return retired, nil
}

func collectCounterWindows(events []data.CounterBumpedDTO, retired map[uint64]bool) (map[uint64]*Loyalty, error) {
	windows := make(map[uint64]*Loyalty)
	for _, event := range events {
		if retired[event.UserID] {
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

func prepareCounterWindows(ctx context.Context, tx *sql.Tx, ids []uint64, windows map[uint64]*Loyalty) (map[bumpKey]*bumpSum, []uint64, error) {
	bumps := make(map[bumpKey]*bumpSum)
	unpromoted := make([]uint64, 0)
	for _, id := range ids {
		window := windows[id]
		if window == nil {
			continue
		}
		needsRecheck, err := redirectCounterTrial(ctx, tx, id, window)
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

func redirectCounterTrial(ctx context.Context, tx *sql.Tx, id uint64, window *Loyalty) (bool, error) {
	trial := batchTrialBumps(id, window.bumpPend)
	if trial == (trialTotals{}) {
		return false, nil
	}
	promoted, err := trialPromoted(ctx, tx, readTrialPromotion, id)
	if err != nil {
		return false, err
	}
	if !promoted {
		return true, nil
	}
	return false, collectTrialCarry(window, id, trial)
}

func collectTrialCarry(window *Loyalty, id uint64, trial trialTotals) error {
	// Keep trial statistics and carry their deltas to the channel counters.
	// The existing collector owns overflow and Unicode/display semantics.
	for _, carry := range trial.carried() {
		bump := data.CounterBumpEntry{Name: carry.name, Scope: data.CounterScopeChannel, Delta: carry.delta}
		if err := window.collectBatchBump(id, bump); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) writeCounterTransaction(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	if err := r.ensureCounterBatchDefinitions(ctx, tx, bumps); err != nil {
		return err
	}
	if err := r.excludeSaturatedCounterRows(ctx, tx, bumps); err != nil {
		return err
	}
	return r.writeCounterSums(ctx, tx, bumps)
}

func (r *Loyalty) verifyTrialPromotions(ctx context.Context, tx *sql.Tx, ids []uint64) error {
	// Recheck follows every trial row write, as in writeCounterBatch. A
	// promotion can commit after our snapshot read while we wait on its locks.
	query := lockTrialPromotion
	if r.dialect == "sqlite3" {
		query = readTrialPromotion
	}
	for _, id := range ids {
		promoted, err := trialPromoted(ctx, tx, query, id)
		if err != nil {
			return err
		}
		if promoted {
			return errTrialPromotedMidBatch
		}
	}
	return nil
}
