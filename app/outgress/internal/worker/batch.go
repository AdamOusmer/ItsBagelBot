package worker

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const batchStateTTL = 2 * time.Minute

const errBatchBusy expectedNackError = "ordered output batch is already owned by another worker"

var (
	errBatchStoreUnavailable = errors.New("outgress batch store unavailable")
	errNestedBatch           = errors.New("nested outgress batch")
)

// BatchStore coordinates one at-most-once batch across outgress replicas.
// Next is the first unclaimed item. SaveNext happens before the Twitch call:
// retries therefore never repeat an ambiguous item, at the accepted cost that
// a crash between checkpoint and send can lose that item.
type BatchStore interface {
	Acquire(context.Context, string, string, time.Duration) (bool, error)
	Next(context.Context, string) (int, error)
	SaveNext(context.Context, string, int, time.Duration) error
	Release(context.Context, string, string) error
}

func (w *Worker) processBatch(ctx context.Context, batch *outgress.Batch, broadcasterID, owner string) error {
	if !batch.Valid() {
		w.log.Error("dropping malformed outgress batch")
		return nil
	}
	if w.batch == nil {
		return errBatchStoreUnavailable
	}

	acquired, err := w.batch.Acquire(ctx, batch.ID, owner, batchStateTTL)
	if err != nil {
		return err
	}
	if !acquired {
		return errBatchBusy
	}

	next, runErr := w.batch.Next(ctx, batch.ID)
	if runErr == nil && next < len(batch.Items) {
		runErr = runBatchItems(batch.Items, next,
			func(next int) error { return w.batch.SaveNext(ctx, batch.ID, next, batchStateTTL) },
			func(item outgress.Message) error {
				if item.Type == outgress.TypeBatch {
					return errNestedBatch
				}
				if item.BroadcasterID == "" {
					item.BroadcasterID = broadcasterID
				}
				return w.processPayload(ctx, item)
			},
		)
	}

	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.batch.Release(releaseCtx, batch.ID, owner); err != nil {
		w.log.Warn("failed to release outgress batch lock", zap.String("batch_id", batch.ID), zap.Error(err))
	}
	return runErr
}

// runBatchItems checkpoints before execution. That is deliberately at-most-
// once: an ambiguous or crashed item is skipped on retry instead of repeated.
func runBatchItems(items []outgress.Message, start int, saveNext func(int) error, execute func(outgress.Message) error) error {
	for i := max(start, 0); i < len(items); i++ {
		if err := saveNext(i + 1); err != nil {
			return err
		}
		if err := execute(items[i]); err != nil {
			return err
		}
	}
	return nil
}
