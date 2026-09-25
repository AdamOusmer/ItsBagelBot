// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/pkg/db"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	// Must exceed sesame's counterPublicationGiveUp plus the BAGEL_DATA and BAGEL_DLQ MaxAge, or a late republish or replay applies twice.
	BatchReceiptRetention = 8 * 24 * time.Hour

	BatchReceiptPruneInterval = 10 * time.Minute

	batchReceiptPruneChunk = 1000

	batchReceiptPruneTimeout = time.Minute
)

const pruneBatchReceipts = "DELETE FROM counter_batches WHERE created_at < ? ORDER BY created_at LIMIT ?"

func (r *Loyalty) PruneBatchReceipts(ctx context.Context, olderThan time.Time) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		deleted, err := r.pruneBatchReceiptChunk(ctx, olderThan)
		total += deleted
		if err != nil || deleted < batchReceiptPruneChunk {
			return total, err
		}
	}
}

func (r *Loyalty) pruneBatchReceiptChunk(ctx context.Context, olderThan time.Time) (int64, error) {
	var deleted int64
	err := retryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			result, err := r.sqldb.ExecContext(ctx, pruneBatchReceipts, olderThan, batchReceiptPruneChunk)
			if err != nil {
				return err
			}
			deleted, err = result.RowsAffected()
			return err
		})
	})
	return deleted, err
}

func (r *Loyalty) StartBatchReceiptPruner(ctx context.Context, interval, retention time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go r.runBatchReceiptPruner(ctx, done, interval, retention)
	return func() {
		cancel()
		<-done
	}
}

func (r *Loyalty) runBatchReceiptPruner(ctx context.Context, done chan<- struct{}, interval, retention time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pruneExpiredBatchReceipts(ctx, retention)
		}
	}
}

func (r *Loyalty) pruneExpiredBatchReceipts(ctx context.Context, retention time.Duration) {
	txn := r.app.StartTransaction("prune counter batch receipts")
	defer txn.End()
	ctx, cancel := context.WithTimeout(newrelic.NewContext(ctx, txn), batchReceiptPruneTimeout)
	defer cancel()

	deleted, err := r.PruneBatchReceipts(ctx, time.Now().Add(-retention))
	if err != nil {
		txn.NoticeError(err)
		r.log.Warn("loyalty: failed to prune counter batch receipts", zap.Int64("deleted", deleted), zap.Error(err))
	}
}
