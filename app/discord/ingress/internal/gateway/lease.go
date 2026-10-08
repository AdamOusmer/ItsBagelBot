// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type Lease interface {
	Acquire(ctx context.Context, ttl time.Duration) (bool, error)
	Renew(ctx context.Context, ttl time.Duration) (bool, error)
	Release(ctx context.Context) error
}

type Role interface {
	Standing(ctx context.Context)
	Leading(ctx context.Context)
}

type leaseTiming struct {
	ttl      time.Duration
	renew    time.Duration
	deadline time.Duration
	poll     time.Duration
	release  time.Duration
}

var defaultLeaseTiming = leaseTiming{
	ttl:      30 * time.Second,
	renew:    5 * time.Second,
	deadline: 20 * time.Second,
	poll:     2 * time.Second,
	release:  2 * time.Second,
}

func (s Session) leaseTiming() leaseTiming {
	if s.timing != nil {
		return *s.timing
	}
	return defaultLeaseTiming
}

func (s Session) runLeased(ctx context.Context) error {
	for {
		if err := s.standby(ctx); err != nil {
			return err
		}
		if err := s.lead(ctx); err != nil {
			return err
		}
	}
}

func (s Session) standby(ctx context.Context) error {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if s.tryAcquire(ctx) {
			return nil
		}
		t.Reset(s.leaseTiming().poll)
	}
}

func (s Session) tryAcquire(ctx context.Context) bool {
	if s.Role != nil {
		s.Role.Standing(ctx)
	}
	won, err := s.takeLease(ctx)
	if err != nil {
		s.log().Warn("discord gateway lease acquire failed", zap.Error(err))
		return false
	}
	if won && s.Role != nil {
		s.Role.Leading(ctx)
	}
	return won
}

func (s Session) takeLease(ctx context.Context) (bool, error) {
	t := s.leaseTiming()
	rctx, cancel := context.WithTimeout(ctx, t.poll)
	defer cancel()
	if kept, err := s.Lease.Renew(rctx, t.ttl); err == nil && kept {
		return true, nil
	}
	return s.Lease.Acquire(rctx, t.ttl)
}

func (s Session) lead(ctx context.Context) error {
	s.log().Info("discord gateway lease acquired")
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	held := make(chan struct{})
	go func() {
		defer close(held)
		if !s.renewWhileHeld(lctx) {
			return
		}
		s.demote(lctx)
		cancel()
	}()
	st := &resumeState{}
	s.loadCheckpoint(lctx, st)
	_ = s.serve(lctx, st)
	cancel()
	<-held
	if ctx.Err() != nil {
		s.drain(ctx, st)
		return ctx.Err()
	}
	return nil
}

func (s Session) demote(ctx context.Context) {
	s.log().Warn("discord gateway lease lost; returning to standby")
	if s.Role != nil {
		s.Role.Standing(ctx)
	}
}

func (s Session) renewWhileHeld(ctx context.Context) (lost bool) {
	t := time.NewTicker(s.leaseTiming().renew)
	defer t.Stop()
	lastKept := s.clock()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
		}
		sent := s.clock()
		kept, err := s.renewOnce(ctx)
		switch {
		case ctx.Err() != nil:
			return false
		case err == nil && !kept:
			return true
		case err == nil:
			lastKept = sent
		case s.clock().Sub(lastKept) >= s.leaseTiming().deadline:
			return true
		}
	}
}

func (s Session) renewOnce(ctx context.Context) (bool, error) {
	rctx, cancel := context.WithTimeout(ctx, s.leaseTiming().renew)
	defer cancel()
	kept, err := s.Lease.Renew(rctx, s.leaseTiming().ttl)
	if err != nil {
		s.log().Warn("discord gateway lease renew failed", zap.Error(err))
	}
	return kept, err
}

func (s Session) drain(ctx context.Context, st *resumeState) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.leaseTiming().release)
	defer cancel()
	s.saveCheckpoint(rctx, st)
	if err := s.Lease.Release(rctx); err != nil {
		s.log().Warn("discord gateway lease release failed", zap.Error(err))
	}
}
