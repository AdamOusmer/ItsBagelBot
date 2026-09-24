// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	valkey "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	trialPromotionEvery = 30 * time.Second
	trialClaimTimeout   = 2 * time.Minute
	trialPromotedDone   = "done"
)

// Claims a promoted trial for one promotion call; a claim older than ARGV[2] seconds is retried.
const claimTrialCounters = `
if redis.call('HGET', KEYS[1], 'state') ~= 'promoted' then return 0 end
local claimed = redis.call('HGET', KEYS[1], 'counters_promoted') or ''
if claimed == 'done' then return 0 end
if claimed ~= '' and tonumber(claimed) > tonumber(ARGV[1]) - tonumber(ARGV[2]) then return 0 end
redis.call('HSET', KEYS[1], 'counters_promoted', ARGV[1])
return 1`

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
	ids, err := t.store.Do(ctx, t.store.B().Zrange().Key("trial:history").Min("0").Max("-1").Build()).AsStrSlice()
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
	key := "trial:channel:" + id
	now := strconv.FormatInt(time.Now().Unix(), 10)
	claimed, err := t.store.Do(ctx, t.store.B().Eval().Script(claimTrialCounters).Numkeys(1).Key(key).
		Arg(now).Arg(strconv.Itoa(int(trialClaimTimeout.Seconds()))).Build()).AsInt64()
	if err != nil || claimed != 1 {
		return err
	}
	userID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return err
	}
	if err := t.promoter.PromoteTrial(ctx, userID); err != nil {
		return err
	}
	return t.store.Do(ctx, t.store.B().Hset().Key(key).FieldValue().FieldValue("counters_promoted", trialPromotedDone).Build()).Error()
}
