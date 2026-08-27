// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

// redemptionAddType is the EventSub type a channel-points redemption arrives
// on (the channelpoints module keeps its own copy; the engine cannot import
// modules without a cycle).
const redemptionAddType = "channel.channel_points_custom_reward_redemption.add"

// refundRedemption is the slice of the redemption.add payload the auto-refund
// gate needs: who redeemed, on whose channel, and the ids Helix wants back to
// cancel it.
type refundRedemption struct {
	ID                   string `json:"id"`
	BroadcasterUserID    string `json:"broadcaster_user_id"`
	BroadcasterUserLogin string `json:"broadcaster_user_login"`
	UserID               string `json:"user_id"`
	Reward               struct {
		ID string `json:"id"`
	} `json:"reward"`
}

// refundSpecialRedemption is the special-user auto-refund gate: on the one
// configured channel (Config.AutoRefundChannel, a Doppler secret), every
// channel-points redemption by a special user is cancelled — Twitch returns
// the points on CANCELED — before any module sees the event. It is a pipeline
// stage, not a registered module, for the same reason automod is: a module
// runs in registration order behind the enable/ModuleView machinery, and this
// must fire even when the channelpoints module is disabled or has no binding
// for the reward. true means the event is consumed: runStages skips the
// handlers so no module acts on (or double-resolves) a refunded redemption.
func (p *Pipeline) refundSpecialRedemption(mctx *module.Context, emit module.Emit) bool {
	if p.autoRefundChannel == "" || mctx.Env.Type != redemptionAddType {
		return false
	}
	var ev refundRedemption
	if codec.Unmarshal(mctx.Env.Event, &ev) != nil {
		return false
	}
	if ev.ID == "" || ev.Reward.ID == "" {
		return false
	}
	if !p.refundChannelMatches(ev) || !p.special.Has(ev.UserID) {
		return false
	}
	emit(&module.Output{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: ev.BroadcasterUserID,
		RewardID:      ev.Reward.ID,
		RedemptionID:  ev.ID,
		Status:        outgress.RedemptionCanceled,
	})
	return true
}

// refundChannelMatches accepts the secret as either the broadcaster's numeric
// user id or their login (case-insensitive, Twitch logins are lowercase): the
// two forms are disjoint (ids are numeric, logins cannot be), so matching both
// costs nothing and keeps the Doppler value human-checkable.
func (p *Pipeline) refundChannelMatches(ev refundRedemption) bool {
	return ev.BroadcasterUserID == p.autoRefundChannel ||
		strings.EqualFold(ev.BroadcasterUserLogin, p.autoRefundChannel)
}
