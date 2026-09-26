// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// StartReconciler dispatches durable due work to the existing bounded workers.
func (s *ValkeyLoyaltyClock) StartReconciler(ctx context.Context) {
	jobs := make(chan uint64, loyaltyQueueSize)
	for range loyaltyWorkers {
		go s.runLoyaltyWorker(ctx, jobs)
	}
	poll := time.NewTicker(loyaltyPollInterval)
	defer poll.Stop()
	reconcile := time.NewTicker(loyaltyReconcileInterval)
	defer reconcile.Stop()
	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcile.C:
			s.reconcile(ctx)
		case <-poll.C:
			s.queueDue(ctx, jobs)
		case <-s.wake:
			s.queueDue(ctx, jobs)
		}
	}
}
func (s *ValkeyLoyaltyClock) runLoyaltyWorker(ctx context.Context, jobs <-chan uint64) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-jobs:
			s.fire(ctx, id)
		case id := <-s.rearms:
			s.rearmChannel(ctx, id)
		}
	}
}
func (s *ValkeyLoyaltyClock) rearmChannel(ctx context.Context, id uint64) {
	rearmCtx, cancel := context.WithTimeout(ctx, loyaltyRearmTimeout)
	defer cancel()
	s.Arm(rearmCtx, id)
}
func (s *ValkeyLoyaltyClock) queueDue(ctx context.Context, jobs chan<- uint64) {
	pctx, cancel := context.WithTimeout(ctx, loyaltyRearmTimeout)
	defer cancel()
	result, err := s.eval(pctx, loyaltyDueScript, []string{loyaltyDueKey}, strconv.FormatInt(s.now().UnixMilli(), 10), strconv.Itoa(loyaltyQueueSize))
	if err != nil {
		s.log.Warn("loyalty: due queue read failed", zap.Error(err))
		return
	}
	ids, err := result.AsStrSlice()
	if err != nil {
		return
	}
	s.enqueueDueIDs(ids, jobs)
}
func (s *ValkeyLoyaltyClock) enqueueDueIDs(ids []string, jobs chan<- uint64) {
	for _, raw := range ids {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			continue
		}
		if id == 0 {
			continue
		}
		select {
		case jobs <- id:
		default:
			return
		}
	}
}
