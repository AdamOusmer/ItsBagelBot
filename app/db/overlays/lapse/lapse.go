// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package lapse

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/overlays/assets"
	"ItsBagelBot/app/db/overlays/repository"
)

const Interval = 6 * time.Hour

type Sweeper struct {
	Repo  *repository.Overlays
	Plans assets.Plans
	Now   func() time.Time
}

func (s *Sweeper) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// Sweep is idempotent: replicas may run it at the same time.
func (s *Sweeper) Sweep(ctx context.Context) error {
	owners, err := s.Repo.CustomOwners(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	var failed error
	for _, userID := range owners {
		failed = errors.Join(failed, s.sweepOwner(ctx, userID, now))
	}
	return failed
}

func (s *Sweeper) sweepOwner(ctx context.Context, userID uint64, now time.Time) error {
	premium, err := s.Plans.Premium(ctx, userID)
	if err != nil {
		return err
	}
	if premium {
		return s.Repo.ClearLapsed(ctx, userID)
	}
	if err := s.Repo.MarkLapsed(ctx, userID, now); err != nil {
		return err
	}
	return s.Repo.PurgeLapsed(ctx, userID, now)
}

func (s *Sweeper) Run(ctx context.Context, log *zap.Logger) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		if err := s.Sweep(ctx); err != nil {
			log.Error("overlay lapse sweep failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
