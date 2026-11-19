// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"go.uber.org/zap"
)

const balanceCreditRetention = 25 * time.Hour

func (r *Loyalty) PruneCounterReceipts(ctx context.Context) error {
	if !r.counterReady.Load() {
		return nil
	}
	return r.pruneReceipts(ctx, receiptPrune{table: "loyalty_counter_receipts", condition: receiptExpiry, args: []any{time.Now().Add(-data.CounterReceiptRetention).Unix()}})
}

func (r *Loyalty) PruneBalanceCredits(ctx context.Context) error {
	if !r.creditReady.Load() {
		return nil
	}
	return r.pruneReceipts(ctx, receiptPrune{table: balanceCreditTable, condition: receiptExpiry, args: []any{time.Now().Add(-balanceCreditRetention).UnixMilli()}})
}

const receiptExpiry = "created_at < ? ORDER BY created_at"

type receiptPrune struct {
	table     string
	condition string
	args      []any
}

func (r *Loyalty) pruneReceipts(ctx context.Context, prune receiptPrune) error {
	ctx, cancel := context.WithTimeout(ctx, watchHistoryPruneBudget)
	defer cancel()
	for range watchHistoryPrunePasses {
		more, err := r.pruneReceiptChunk(ctx, prune)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (r *Loyalty) pruneReceiptChunk(ctx context.Context, prune receiptPrune) (bool, error) {
	where := " WHERE " + prune.condition + " LIMIT ?"
	query := "DELETE FROM " + prune.table + where
	if r.dialect == "sqlite3" {
		query = "DELETE FROM " + prune.table + " WHERE rowid IN (SELECT rowid FROM " + prune.table + where + ")"
	}
	result, err := r.sqldb.ExecContext(ctx, query, append(prune.args, watchHistoryPruneBatch)...)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == watchHistoryPruneBatch, err
}

func (r *Loyalty) maintainReceipts(ctx context.Context) {
	r.reportReceiptPruning(r.PruneCounterReceipts(ctx), "counter")
	r.reportReceiptPruning(r.PruneBalanceCredits(ctx), "balance credit")
	r.reportReceiptPruning(r.PruneFreezeSnapshots(ctx), "freeze snapshot")
}

func (r *Loyalty) reportReceiptPruning(err error, kind receiptKind) {
	if err == nil || r.log == nil {
		return
	}
	r.log.Warn("loyalty: receipt pruning failed", zap.String("kind", string(kind)), zap.Error(err))
}

type receiptKind string
