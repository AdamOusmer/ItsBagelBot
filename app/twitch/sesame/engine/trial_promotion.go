// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	valkey "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	trialPromotionEvery = 30 * time.Second
	trialClaimTimeout   = 2 * time.Minute
	trialPromotedDone   = "done"
)

// Claims a promoted trial's counters once; a claim older than ARGV[2] seconds is retried.
const claimTrialCounters = `
if redis.call('HGET', KEYS[1], 'state') ~= 'promoted' then return false end
local claimed = redis.call('HGET', KEYS[1], 'counters_promoted') or ''
if claimed == 'done' then return false end
if claimed ~= '' and tonumber(claimed) > tonumber(ARGV[1]) - tonumber(ARGV[2]) then return false end
redis.call('HSET', KEYS[1], 'counters_promoted', ARGV[1])
return {redis.call('HGET', KEYS[1], 'generation') or '', redis.call('HGET', KEYS[1], 'decoded') or '0', redis.call('HGET', KEYS[1], 'answered') or '0'}`

type TrialPromotion struct {
	store valkey.Client
	pub   bus.Publisher
	log   *zap.Logger
}

func NewTrialPromotion(store valkey.Client, pub bus.Publisher, log *zap.Logger) *TrialPromotion {
	return &TrialPromotion{store: store, pub: pub, log: log}
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
	claim, err := t.store.Do(ctx, t.store.B().Eval().Script(claimTrialCounters).Numkeys(1).Key(key).
		Arg(now).Arg(strconv.Itoa(int(trialClaimTimeout.Seconds()))).Build()).AsStrSlice()
	if valkey.IsValkeyNil(err) || len(claim) != 3 {
		return nil
	}
	if err != nil {
		return err
	}
	userID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return err
	}
	if err := t.publish(ctx, userID, claim); err != nil {
		return err
	}
	return t.store.Do(ctx, t.store.B().Hset().Key(key).FieldValue().FieldValue("counters_promoted", trialPromotedDone).Build()).Error()
}

func (t *TrialPromotion) publish(ctx context.Context, userID uint64, claim []string) error {
	decoded, _ := strconv.ParseInt(claim[1], 10, 64)
	answered, _ := strconv.ParseInt(claim[2], 10, 64)
	bumps := make([]data.CounterBumpEntry, 0, 3)
	for _, b := range []data.CounterBumpEntry{
		{Name: counterEventsProcessed, Delta: decoded},
		{Name: counterMessagesProcessed, Delta: decoded},
		{Name: counterCommandsAnswered, Delta: answered},
	} {
		if b.Delta > 0 {
			b.Scope = data.CounterScopeChannel
			bumps = append(bumps, b)
		}
	}
	if len(bumps) == 0 {
		return nil
	}
	body, err := codec.Marshal(data.CounterBumpedDTO{UserID: userID, Bumps: bumps})
	if err != nil {
		return err
	}
	return bus.PublishConfirmed(ctx, t.pub, bus.Publication{
		Subject: data.SubjectLoyaltyCounters,
		ID:      "trial-promote:" + strconv.FormatUint(userID, 10) + ":" + claim[0],
		Payload: body,
	})
}
