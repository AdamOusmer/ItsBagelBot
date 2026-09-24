// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

const redemptionAddType = "channel.channel_points_custom_reward_redemption.add"

type refundRedemption struct {
	ID                   string `json:"id"`
	BroadcasterUserID    string `json:"broadcaster_user_id"`
	BroadcasterUserLogin string `json:"broadcaster_user_login"`
	UserID               string `json:"user_id"`
	Reward               struct {
		ID string `json:"id"`
	} `json:"reward"`
}

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

func (p *Pipeline) refundChannelMatches(ev refundRedemption) bool {
	return ev.BroadcasterUserID == p.autoRefundChannel ||
		strings.EqualFold(ev.BroadcasterUserLogin, p.autoRefundChannel)
}
