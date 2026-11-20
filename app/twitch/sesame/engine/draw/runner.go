// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	sweepPoll = 5 * time.Second
	leaseTTL  = 15 * time.Second
	dueBatch  = 100
)

func (s *Store) Run(ctx context.Context) {
	wake := make(chan struct{}, 1)
	go s.watch(ctx, wake)
	steps := phaseSteps()
	for ctx.Err() == nil {
		delay := s.sweep(ctx, steps)
		wait(ctx, wake, delay)
	}
}

func (s *Store) watch(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		err := valkey.OnPublish(ctx, s.client, wakeChannel, func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		})
		if ctx.Err() != nil {
			return
		}
		s.cfg.Log.Warn("draw wake subscription stopped", zap.Error(err))
		wait(ctx, nil, sweepPoll)
	}
}

func wait(ctx context.Context, wake <-chan struct{}, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-wake:
	case <-timer.C:
	}
}

func (s *Store) sweep(ctx context.Context, steps map[Phase]roundStep) time.Duration {
	ids, next, err := s.due.Lapsed(ctx, s.client, dueBatch)
	if err != nil {
		s.logFailure(ctx, "", err)
		return sweepPoll
	}
	delay := sweepPoll
	if next >= 0 {
		delay = min(next+time.Millisecond, delay)
	}
	if s.runDue(ctx, ids, steps) {
		return 0
	}
	return delay
}

func (s *Store) runDue(ctx context.Context, ids []string, steps map[Phase]roundStep) bool {
	var progress atomic.Bool
	var work sync.WaitGroup
	for _, raw := range ids {
		work.Go(func() {
			id := RoundID(raw)
			advanced, err := s.runRound(ctx, id, steps)
			s.logFailure(ctx, id, err)
			if advanced {
				progress.Store(true)
			}
		})
	}
	work.Wait()
	return progress.Load()
}

func (s *Store) runRound(ctx context.Context, id RoundID, steps map[Phase]roundStep) (bool, error) {
	work, cancel := context.WithTimeout(ctx, leaseTTL-time.Second)
	defer cancel()
	lock := valkey.NewOwnerLock(s.client, leaseKey(id), uuid.NewString())
	ok, err := lock.Acquire(work, leaseTTL)
	if err != nil || !ok {
		return false, err
	}
	err = s.stepRound(work, id, steps)
	release, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer stop()
	releaseErr := lock.Release(release)
	return err == nil, errors.Join(err, releaseErr)
}

func (s *Store) stepRound(ctx context.Context, id RoundID, steps map[Phase]roundStep) error {
	data, err := s.read(ctx, id)
	if err != nil {
		return err
	}
	if !data.stored.Found {
		return s.due.Remove(ctx, s.client, string(id))
	}
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return err
	}
	step := steps[data.round.Phase]
	if step == nil {
		return fmt.Errorf("draw: unknown phase %q", data.round.Phase)
	}
	err = step(ctx, s, data, now)
	if err != nil {
		retry, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		err = errors.Join(err, s.schedule(retry, data, sweepPoll))
	}
	return err
}

func (s *Store) schedule(ctx context.Context, data snapshot, delay time.Duration) error {
	r := data.round
	return keysFor(r.Channel, r.ID).Schedule(ctx, s.client, valkey.DrawSchedule{
		RoundID: string(r.ID), Revision: data.stored.Round["revision"], After: max(delay, time.Millisecond), TTL: roundTTL,
	})
}

func (s *Store) logFailure(ctx context.Context, round RoundID, err error) {
	if err != nil && ctx.Err() == nil {
		s.cfg.Log.Warn("draw due step failed", zap.String("round", string(round)), zap.Error(err))
	}
}
