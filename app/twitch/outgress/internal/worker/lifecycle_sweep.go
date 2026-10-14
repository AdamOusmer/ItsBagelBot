// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"go.uber.org/zap"
)

const (
	unhealthySweepInterval = 5 * time.Minute
	unhealthySweepBatch    = 25
	unhealthyVerifyTimeout = 5 * time.Second
)

func (w *Worker) sweepUnhealthy(ctx context.Context) {
	claimed, err := w.registry.ClaimUnhealthySweep(ctx, unhealthySweepInterval)
	if err != nil || !claimed {
		return
	}
	ids, err := w.registry.UnhealthyBatch(ctx, unhealthySweepBatch)
	if err != nil {
		w.log.Warn("unhealthy sweep: list failed", zap.Error(err))
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		w.verifyUnhealthy(ctx, id)
	}
}

func (w *Worker) verifyUnhealthy(ctx context.Context, id string) {
	callCtx, cancel := context.WithTimeout(ctx, unhealthyVerifyTimeout)
	defer cancel()
	if _, err := w.VerifyChannel(callCtx, id, true); err != nil {
		w.log.Warn("unhealthy sweep: verify failed", zap.String("broadcaster_id", id), zap.Error(err))
	}
}
