// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"go.uber.org/zap"
)

const channelPointsModuleName = "channelpoints"

const (
	rewardActionChat = "chat"
	rewardActionNone = "none"
)

const (
	onRedeemFulfill = "fulfill"
	onRedeemCancel  = "cancel"
	onRedeemLeave   = "leave"
)

const defaultRewardChatTemplate = "{channelpoints:user} redeemed {channelpoints:reward}!"

type channelPointsConfig struct {
	Rewards []rewardBinding `json:"rewards"`
}

type rewardBinding struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	Message  string `json:"message"`
	OnRedeem string `json:"onRedeem"`
	Counter  string `json:"counter"`
	Points   int64  `json:"points"`
	LiveOnly bool   `json:"liveOnly"`
}

func ChannelPoints(d engine.Deps) module.Module {
	m := module.NewModule(channelPointsModuleName, module.KindOptIn).ProtectReplay()

	m.On(engine.RedemptionAddType, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var cfg channelPointsConfig
		_ = c.Decode(&cfg)
		if len(cfg.Rewards) == 0 || len(c.Env.Event) == 0 {
			return nil
		}

		var ev engine.RedemptionEvent
		if err := codec.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.Reward.ID == "" || ev.BroadcasterUserID == "" {
			return nil
		}

		binding, ok := findBinding(cfg.Rewards, ev.Reward.ID)
		if !ok {
			return nil
		}

		var counterValue string
		result := "offline"
		if loyaltyLive(ctx, d, c.BroadcasterID, binding.LiveOnly) {
			result = "ok"
			award := rewardAward{d: d, c: c, binding: binding, ev: ev}
			award.grant(ctx)
			counterValue = award.bumpCounter(ctx)
		}
		d.Analytics.AddContext(ctx, catalog.RewardsRedeems, 1, "channelpoints", result)
		emitRewardAction(rewardChatParams{
			locale:       c.Locale,
			binding:      binding,
			event:        ev,
			counterValue: counterValue,
		}, emit)
		emitRedemptionResolution(binding, ev, emit)
		return nil
	})

	return m.Build()
}

func loyaltyLive(ctx context.Context, d engine.Deps, broadcasterID uint64, liveOnly bool) bool {
	if !liveOnly || d.Live == nil {
		return true
	}
	live, err := d.Live.IsLive(ctx, broadcasterID)
	return err == nil && live
}

type rewardAward struct {
	d       engine.Deps
	c       *module.Context
	binding rewardBinding
	ev      engine.RedemptionEvent
}

func (a rewardAward) grant(ctx context.Context) {
	if a.binding.Points <= 0 || a.d.Loyalty == nil {
		return
	}
	viewerID, err := strconv.ParseUint(a.ev.UserID, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	points := a.points(ctx, viewerID)
	if points <= 0 {
		return
	}
	err = a.d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&a.c.Env), Effect: engine.EffectEarn}, func(ctx context.Context) error {
		a.d.Loyalty.Earn(engine.PointEarning{Context: ctx, BroadcasterID: a.c.BroadcasterID, ViewerID: viewerID, Login: a.ev.UserLogin, Name: a.ev.UserName, Points: points})
		a.d.Analytics.AddContext(ctx, catalog.PointsFlows, 1, "reward_grant")
		return nil
	})
	idempotency.Fail(ctx, err)
}

func (a rewardAward) points(ctx context.Context, viewerID uint64) int64 {
	if a.c.BroadcasterID != viewerID || a.d.Proj == nil {
		return a.binding.Points
	}
	cfg, _ := engine.ReadLoyaltyConfig(ctx, a.d.Proj, a.c.BroadcasterID)
	return cfg.AutomaticPoints(a.c.BroadcasterID, viewerID, a.binding.Points)
}

func (a rewardAward) bumpCounter(ctx context.Context) string {
	d, c, b, ev := a.d, a.c, a.binding, a.ev
	if b.Counter == "" || d.Loyalty == nil {
		return ""
	}
	viewerID, _ := strconv.ParseUint(ev.UserID, 10, 64)
	viewer := engine.Viewer{ID: viewerID, Login: ev.UserLogin, Name: ev.UserName}
	bump := engine.CounterBump{
		BroadcasterID: c.BroadcasterID,
		Name:          b.Counter,
		Viewer:        viewer,
		Command:       ev.Reward.Title,
		Delta:         1,
	}
	var value int64
	err := d.Dedup.Effect(ctx, engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: engine.CounterEffect(b.Counter)}, func(ctx context.Context) error {
		var err error
		value, err = d.Loyalty.CounterBump(ctx, bump)
		return err
	})
	if err != nil {
		idempotency.Fail(ctx, err)
		if d.Log != nil {
			d.Log.Warn("channelpoints: counter bump failed",
				c.BID(),
				zap.String("counter", b.Counter),
				zap.Error(err),
			)
		}
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func findBinding(rewards []rewardBinding, rewardID string) (rewardBinding, bool) {
	for _, r := range rewards {
		if r.ID == rewardID {
			return r, true
		}
	}
	return rewardBinding{}, false
}

type rewardChatParams struct {
	locale       string
	binding      rewardBinding
	event        engine.RedemptionEvent
	counterValue string
}

func emitRewardAction(p rewardChatParams, emit module.Emit) {
	if p.binding.Action != rewardActionChat {
		return
	}
	if msg := expandReward(p); msg != "" {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: p.event.BroadcasterUserID, Text: msg})
	}
}

func emitRedemptionResolution(b rewardBinding, ev engine.RedemptionEvent, emit module.Emit) {
	switch b.OnRedeem {
	case onRedeemFulfill:
		emit(ev.Resolve(outgress.RedemptionFulfilled))
	case onRedeemCancel:
		emit(ev.Resolve(outgress.RedemptionCanceled))
	}
}

func sanitizeRewardInput(raw string) string {
	return strings.TrimLeft(raw, " /")
}

func expandReward(p rewardChatParams) string {
	ev := p.event
	kv := []string{
		"user", strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@"),
		"input", sanitizeRewardInput(ev.UserInput),
		"reward", ev.Reward.Title,
		"cost", strconv.Itoa(ev.Reward.Cost),
		"channel", ev.BroadcasterUserLogin,
	}
	if p.counterValue != "" {
		kv = append(kv, "counter", p.counterValue)
	}
	if p.binding.Points > 0 {
		kv = append(kv, "points", strconv.FormatInt(p.binding.Points, 10))
	}
	text := orDefault(p.binding.Message, defaultRewardChatTemplate)
	return module.KV(kv...).WithLocale(module.Locale(p.locale)).WithNamespace("channelpoints").ExpandString(text)
}
