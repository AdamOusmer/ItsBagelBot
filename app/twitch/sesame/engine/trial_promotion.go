// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	valkey "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	trialPromotionEvery = 30 * time.Second
	trialClaimTimeout   = 2 * time.Minute
	trialPromotedDone   = "done"
)

type TrialPromoter interface {
	PromoteTrial(ctx context.Context, broadcasterID uint64) error
}

type TrialPromotion struct {
	store    valkey.Client
	promoter TrialPromoter
	log      *zap.Logger
}

func NewTrialPromotion(store valkey.Client, promoter TrialPromoter, log *zap.Logger) *TrialPromotion {
	return &TrialPromotion{store: store, promoter: promoter, log: log}
}

func (t *TrialPromotion) Run(ctx context.Context) {
	ticker := time.NewTicker(trialPromotionEvery)
	defer ticker.Stop()
	for {
		t.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (t *TrialPromotion) Sweep(ctx context.Context) {
	ids, err := pkg_valkey.ZRangeAll(ctx, t.store, "trial:history")
	if err != nil {
		t.log.Debug("trial promotion sweep unavailable", zap.Error(err))
		return
	}
	for _, id := range ids {
		if err := t.promote(ctx, id); err != nil {
			t.log.Warn("trial counter promotion failed", zap.String("broadcaster_id", id), zap.Error(err))
		}
	}
}

func (t *TrialPromotion) promote(ctx context.Context, id string) error {
	claim := pkg_valkey.AgedClaim{
		Key:       "trial:channel:" + id,
		Guard:     "state",
		Want:      "promoted",
		Field:     "counters_promoted",
		DoneValue: trialPromotedDone,
		MaxAge:    trialClaimTimeout,
	}
	claimed, err := claim.Claim(ctx, t.store, time.Now())
	if err != nil || !claimed {
		return err
	}
	userID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return err
	}
	if err := t.promoter.PromoteTrial(ctx, userID); err != nil {
		return err
	}
	return claim.Done(ctx, t.store)
}
