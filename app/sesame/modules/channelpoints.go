package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
)

// channelPointsModuleName is the ModuleView key. The dashboard's Channel Points
// tab (not the modules page) writes this row: its enable toggle gates the module
// and its Configs blob carries the reward->action bindings this handler reads.
const channelPointsModuleName = "channelpoints"

// redemptionAddType is the EventSub type a channel-points redemption arrives on.
// Registering a handler for it makes the registry mark the type as needing the
// broadcaster's ModuleView fetched, so the bindings blob reaches this handler.
const redemptionAddType = "channel.channel_points_custom_reward_redemption.add"

// Reward action kinds. "chat" posts the binding's message; "none" (or anything
// unknown) runs nothing, leaving only the onRedeem policy to act.
const (
	rewardActionChat = "chat"
	rewardActionNone = "none"
)

// Redemption resolution policies (what to do with the redemption in Twitch's
// request queue after the action runs). "leave" leaves it UNFULFILLED for a mod.
const (
	onRedeemFulfill = "fulfill"
	onRedeemCancel  = "cancel"
	onRedeemLeave   = "leave"
)

const defaultRewardChatTemplate = "{user} redeemed {reward}!"

// channelPointsConfig is the module's dashboard configuration: one binding per
// reward the broadcaster wants the bot to react to. The dashboard owns the
// Twitch-side reward (via the outgress channelpoints RPC) and mirrors the
// reward->action mapping here so sesame can act on a redemption without any RPC
// of its own.
type channelPointsConfig struct {
	Rewards []rewardBinding `json:"rewards"`
}

// rewardBinding maps one Twitch custom reward id to the action the bot runs when
// it is redeemed, plus how to resolve the redemption afterward.
type rewardBinding struct {
	// ID is the Twitch custom reward id (event.reward.id).
	ID string `json:"id"`
	// Action is rewardActionChat or rewardActionNone; empty/unknown = none.
	Action string `json:"action"`
	// Message is the chat template; tokens {user} {input} {reward} {cost}
	// {channel} plus the dynamic {random}/{choice:...} set.
	Message string `json:"message"`
	// OnRedeem is the resolution policy (fulfill/cancel/leave); empty = leave.
	OnRedeem string `json:"onRedeem"`
}

// redemptionEvent is the subset of the redemption.add EventSub payload we use.
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

// ChannelPoints reacts to Twitch channel-points redemptions. It is a named,
// opt-in module (KindOptIn): off by default, enabled from the dashboard's
// Channel Points tab once the broadcaster has created a reward there. On a
// redemption it looks up the reward's binding and, if configured, posts a chat
// line, then optionally resolves the redemption in Twitch's queue (fulfill /
// cancel-refund) via an outgress redemption_update.
//
// It owns no commands: a redemption is not a chat command, so there is nothing
// to gate on the command path — the module is purely event-driven.
func ChannelPoints(_ engine.Deps) module.Module {
	m := module.NewModule(channelPointsModuleName, module.KindOptIn)

	m.On(redemptionAddType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		var cfg channelPointsConfig
		_ = c.Decode(&cfg)
		if len(cfg.Rewards) == 0 || len(c.Env.Event) == 0 {
			return nil
		}

		var ev redemptionEvent
		if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
			return err
		}
		if ev.Reward.ID == "" || ev.BroadcasterUserID == "" {
			return nil
		}

		binding, ok := findBinding(cfg.Rewards, ev.Reward.ID)
		if !ok {
			return nil
		}

		emitRewardAction(binding, ev, emit)
		emitRedemptionResolution(binding, ev, emit)
		return nil
	})

	return m.Build()
}

// findBinding returns the binding for a redeemed reward id, if the broadcaster
// configured one.
func findBinding(rewards []rewardBinding, rewardID string) (rewardBinding, bool) {
	for _, r := range rewards {
		if r.ID == rewardID {
			return r, true
		}
	}
	return rewardBinding{}, false
}

// emitRewardAction runs the binding's chat action. A "none" (or unknown) action
// does nothing, leaving only the resolution policy to act.
func emitRewardAction(b rewardBinding, ev redemptionEvent, emit module.Emit) {
	if b.Action != rewardActionChat {
		return
	}
	if msg := expandReward(orDefault(b.Message, defaultRewardChatTemplate), ev); msg != "" {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: ev.BroadcasterUserID, Text: msg})
	}
}

// emitRedemptionResolution resolves the redemption in Twitch's queue per the
// binding's policy. "leave" (or empty) emits nothing, leaving it for a human mod.
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

// expandReward substitutes the reward template tokens: {user} the redeemer's
// display name, {input} the text they typed, {reward} the reward title, {cost}
// the point cost, {channel} the broadcaster login, plus the dynamic set.
func expandReward(tmpl string, ev redemptionEvent) string {
	return module.ExpandString(tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@"), true
		case "input":
			return ev.UserInput, true
		case "reward":
			return ev.Reward.Title, true
		case "cost":
			return strconv.Itoa(ev.Reward.Cost), true
		case "channel":
			return ev.BroadcasterUserLogin, true
		default:
			return module.ParseDynamic(key)
		}
	})
}
