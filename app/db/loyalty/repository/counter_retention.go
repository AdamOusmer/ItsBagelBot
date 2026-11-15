// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"go.uber.org/zap"
)

func counterReceiptRetention() time.Duration {
	window := data.CounterReceiptTTL + bus.DataStream.MaxAge
	return (window + time.Hour - 1) / time.Hour * time.Hour
}

func (r *Loyalty) PruneCounterReceipts(ctx context.Context) error {
	if !r.counterReady.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, watchHistoryPruneBudget)
	defer cancel()
	cutoff := time.Now().Add(-counterReceiptRetention()).Unix()
	for range watchHistoryPrunePasses {
		more, err := r.pruneCounterReceiptChunk(ctx, cutoff)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (r *Loyalty) pruneCounterReceiptChunk(ctx context.Context, cutoff int64) (bool, error) {
	query := "DELETE FROM loyalty_counter_receipts WHERE created_at < ? ORDER BY created_at LIMIT ?"
	if r.dialect == "sqlite3" {
		query = "DELETE FROM loyalty_counter_receipts WHERE rowid IN (SELECT rowid FROM loyalty_counter_receipts WHERE created_at < ? ORDER BY created_at LIMIT ?)"
	}
	result, err := r.sqldb.ExecContext(ctx, query, cutoff, watchHistoryPruneBatch)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == watchHistoryPruneBatch, err
}

func (r *Loyalty) maintainCounterReceipts(ctx context.Context) {
	err := r.PruneCounterReceipts(ctx)
	if err == nil || r.log == nil {
		return
	}
	r.log.Warn("loyalty: counter receipt pruning failed", zap.Error(err))
}
