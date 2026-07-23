package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/app/sesame/engine"
	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
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
	// {channel} {counter} plus the dynamic {random}/{choice:...} set.
	Message string `json:"message"`
	// OnRedeem is the resolution policy (fulfill/cancel/leave); empty = leave.
	OnRedeem string `json:"onRedeem"`
	// Counter, when set, bumps that loyalty counter by one per redemption (a
	// viewer-scoped counter bumps the redeemer's own value). The new value is
	// exposed to Message as {counter}.
	Counter string `json:"counter"`
	// Points, when positive, awards that many loyalty points to the redeemer
	// per redemption — channel points buying channel currency. Exposed to
	// Message as {points}.
	Points int64 `json:"points"`
	// LiveOnly gates the loyalty writes (counter bump + points award) to when
	// the broadcaster is live, so channel points redeemed offline cannot farm
	// currency or inflate a counter. The chat reply and queue resolution still
	// run either way.
	LiveOnly bool `json:"liveOnly"`
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
func ChannelPoints(d engine.Deps) module.Module {
	m := module.NewModule(channelPointsModuleName, module.KindOptIn)

	m.On(redemptionAddType, func(ctx context.Context, c *module.Context, emit module.Emit) error {
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

		// Loyalty writes are the only live-gated part; the chat reply and queue
		// resolution always run. Skipping the counter bump also skips its
		// {counter} value, so the template renders without the token — the same
		// as an unbound counter.
		var counterValue string
		if loyaltyLive(ctx, d, c.BroadcasterID, binding.LiveOnly) {
			awardRewardPoints(d, c, binding, ev)
			counterValue = bumpRewardCounter(ctx, d, c, binding, ev)
		}
		emitRewardAction(binding, ev, counterValue, emit)
		emitRedemptionResolution(binding, ev, emit)
		return nil
	})

	return m.Build()
}

// loyaltyLive reports whether the binding's loyalty writes may run: always
// when the binding is not live-gated, otherwise only while the broadcaster is
// live. A live-check error fails closed (skip the writes) so an offline redeem
// is never credited on a transient read failure. A nil live store passes (the
// gate has nothing to consult).
func loyaltyLive(ctx context.Context, d engine.Deps, broadcasterID uint64, liveOnly bool) bool {
	if !liveOnly || d.Live == nil {
		return true
	}
	live, err := d.Live.IsLive(ctx, broadcasterID)
	return err == nil && live
}

// awardRewardPoints hands the binding's loyalty-point award (if any) to the
// redeemer — fire-and-forget through the loyalty reporter, like every accrual.
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

// bumpRewardCounter bumps the binding's loyalty counter (if any) once for this
// redemption and returns the new value for the {counter} template token. The
// reward title keys a viewer+command counter's bucket the same way a command's
// canonical name does — Twitch enforces unique custom-reward titles per
// channel, so the title is the reward's name in exactly the sense a trigger is
// a command's. A failure (or no loyalty store) returns "" so the chat action
// still runs.
func bumpRewardCounter(ctx context.Context, d engine.Deps, c *module.Context, b rewardBinding, ev redemptionEvent) string {
	if b.Counter == "" || d.Loyalty == nil {
		return ""
	}
	viewerID, _ := strconv.ParseUint(ev.UserID, 10, 64)
	value, err := d.Loyalty.CounterBump(ctx, c.BroadcasterID, b.Counter, viewerID, ev.Reward.Title, 1)
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("channelpoints: counter bump failed",
				zap.Uint64("broadcaster_id", c.BroadcasterID),
				zap.String("counter", b.Counter),
				zap.Error(err),
			)
		}
		return ""
	}
	return strconv.FormatInt(value, 10)
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
func emitRewardAction(b rewardBinding, ev redemptionEvent, counterValue string, emit module.Emit) {
	if b.Action != rewardActionChat {
		return
	}
	if msg := expandReward(orDefault(b.Message, defaultRewardChatTemplate), ev, counterValue, b.Points); msg != "" {
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
// the point cost, {channel} the broadcaster login, {counter} the bound
// counter's new value (when the binding has one), {points} the loyalty points
// the binding awards (when positive), plus the dynamic set.
func expandReward(tmpl string, ev redemptionEvent, counterValue string, points int64) string {
	return module.ExpandString(tmpl, func(key string) (string, bool) {
		switch key {
		case "user":
			return strings.TrimPrefix(displayName(ev.UserName, ev.UserLogin), "@"), true
		case "input":
			// {input} is viewer-typed text. With slash-verbs now routed on
			// every emit path, a template leading with {input} must not let a
			// redeemer mint /announce (or any verb) as the bot: strip a
			// leading slash/space run, mirroring the engine's sanitizeVar for
			// command {args}. Non-leading slashes (URLs) are untouched.
			return strings.TrimLeft(ev.UserInput, " /"), true
		case "reward":
			return ev.Reward.Title, true
		case "cost":
			return strconv.Itoa(ev.Reward.Cost), true
		case "channel":
			return ev.BroadcasterUserLogin, true
		case "counter":
			return counterValue, counterValue != ""
		case "points":
			return strconv.FormatInt(points, 10), points > 0
		default:
			return module.ParseDynamic(key)
		}
	})
}
