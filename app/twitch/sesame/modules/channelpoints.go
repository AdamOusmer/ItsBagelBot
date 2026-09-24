// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const channelPointsModuleName = "channelpoints"

const redemptionAddType = "channel.channel_points_custom_reward_redemption.add"

const (
	rewardActionChat = "chat"
	rewardActionNone = "none"
)

const (
	onRedeemFulfill = "fulfill"
	onRedeemCancel  = "cancel"
	onRedeemLeave   = "leave"
)

const defaultRewardChatTemplate = "{user} redeemed {reward}!"

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

type redemptionEvent struct {
	ID                   string `json:"id"`
	BroadcasterUserID    string `json:"broadcaster_user_id"`
	BroadcasterUserLogin string `json:"broadcaster_user_login"`
	UserID               string `json:"user_id"`
	UserName             string `json:"user_name"`
	UserLogin            string `json:"user_login"`
	UserInput            string `json:"user_input"`
	Reward               struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Cost  int    `json:"cost"`
	} `json:"reward"`
}

func ChannelPoints(d engine.Deps) module.Module {
	m := module.NewModule(channelPointsModuleName, module.KindOptIn)

	m.On(redemptionAddType, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var cfg channelPointsConfig
		_ = c.Decode(&cfg)
		if len(cfg.Rewards) == 0 || len(c.Env.Event) == 0 {
			return nil
		}

		var ev redemptionEvent
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
		if loyaltyLive(ctx, d, c.BroadcasterID, binding.LiveOnly) {
			awardRewardPoints(d, c, binding, ev)
			counterValue = bumpRewardCounter(ctx, d, c, binding, ev)
		}
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

func awardRewardPoints(d engine.Deps, c *module.Context, b rewardBinding, ev redemptionEvent) {
	if b.Points <= 0 || d.Loyalty == nil {
		return
	}
	viewerID, err := strconv.ParseUint(ev.UserID, 10, 64)
	if err != nil || viewerID == 0 {
		return
	}
	d.Loyalty.Earn(c.BroadcasterID, viewerID, ev.UserLogin, ev.UserName, b.Points, 0)
}

func bumpRewardCounter(ctx context.Context, d engine.Deps, c *module.Context, b rewardBinding, ev redemptionEvent) string {
	if b.Counter == "" || d.Loyalty == nil {
		return ""
	}
	viewerID, _ := strconv.ParseUint(ev.UserID, 10, 64)
	viewer := engine.Viewer{ID: viewerID, Login: ev.UserLogin, Name: ev.UserName}
	value, err := d.Loyalty.CounterBump(ctx, engine.CounterBump{
		BroadcasterID: c.BroadcasterID,
		Name:          b.Counter,
		Viewer:        viewer,
		Command:       ev.Reward.Title,
		Delta:         1,
	})
	if err != nil {
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
	event        redemptionEvent
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

func emitRedemptionResolution(b rewardBinding, ev redemptionEvent, emit module.Emit) {
	var status string
	switch b.OnRedeem {
	case onRedeemFulfill:
		status = outgress.RedemptionFulfilled
	case onRedeemCancel:
		status = outgress.RedemptionCanceled
	default:
		return
	}
	emit(&module.Output{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: ev.BroadcasterUserID,
		RewardID:      ev.Reward.ID,
		RedemptionID:  ev.ID,
		Status:        status,
	})
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
	return module.KV(kv...).WithLocale(module.Locale(p.locale)).ExpandString(text)
}
