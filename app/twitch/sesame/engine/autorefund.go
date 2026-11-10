// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strings"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

func (p *Pipeline) refundSpecialRedemption(mctx *module.Context, emit module.Emit) bool {
	if p.autoRefundChannel == "" || mctx.Env.Type != RedemptionAddType {
		return false
	}
	var ev RedemptionEvent
	if codec.Unmarshal(mctx.Env.Event, &ev) != nil {
		return false
	}
	if ev.ID == "" || ev.Reward.ID == "" {
		return false
	}
	if !p.refundChannelMatches(ev) || !p.special.Has(ev.UserID) {
		return false
	}
	emit(ev.Resolve(outgress.RedemptionCanceled))
	return true
}

func (p *Pipeline) refundChannelMatches(ev RedemptionEvent) bool {
	return ev.BroadcasterUserID == p.autoRefundChannel ||
		strings.EqualFold(ev.BroadcasterUserLogin, p.autoRefundChannel)
}
