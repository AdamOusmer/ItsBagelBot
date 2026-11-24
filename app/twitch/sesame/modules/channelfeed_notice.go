// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/pkg/codec"
)

const (
	noticeGiftPaidUpgrade  = "gift_paid_upgrade"
	noticePrimePaidUpgrade = "prime_paid_upgrade"
	noticePayItForward     = "pay_it_forward"
)

type feedGifter struct {
	IsAnonymous bool   `json:"gifter_is_anonymous"`
	UserID      string `json:"gifter_user_id"`
	UserLogin   string `json:"gifter_user_login"`
	UserName    string `json:"gifter_user_name"`
}

func (g *feedGifter) user() *channelevents.User {
	if g == nil || g.IsAnonymous {
		return nil
	}
	return feedIdentity{UserID: g.UserID, UserLogin: g.UserLogin, UserName: g.UserName}.user()
}

// feedNotice holds the channel.chat.notification subtypes that have no channel.* twin.
// sub, resub, sub_gift and community_sub_gift repeat channel.subscribe, channel.subscription.message and
// channel.subscription.gift, so publishing them again would double those events.
type feedNotice struct {
	ChatterUserID    string      `json:"chatter_user_id"`
	ChatterUserLogin string      `json:"chatter_user_login"`
	ChatterUserName  string      `json:"chatter_user_name"`
	NoticeType       string      `json:"notice_type"`
	GiftPaidUpgrade  *feedGifter `json:"gift_paid_upgrade"`
	PayItForward     *feedGifter `json:"pay_it_forward"`
	PrimePaidUpgrade *struct {
		Tier string `json:"sub_tier"`
	} `json:"prime_paid_upgrade"`
}

func onFeedNotice(d engine.Deps) module.EventHandler {
	announce := feedHandler(d, channelevents.GiftUpgrade, buildNotice)
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var in feedNotice
		if err := codec.Unmarshal(c.Env.Event, &in); err != nil || !in.isUpgrade() {
			return err
		}
		return announce(ctx, c, emit)
	}
}

func (n feedNotice) isUpgrade() bool {
	switch n.NoticeType {
	case noticeGiftPaidUpgrade, noticePrimePaidUpgrade, noticePayItForward:
		return true
	}
	return false
}

func (n feedNotice) gifter() *feedGifter {
	if n.GiftPaidUpgrade != nil {
		return n.GiftPaidUpgrade
	}
	return n.PayItForward
}

func buildNotice(event channelevents.Event, in feedNotice) channelevents.Event {
	event.User = feedIdentity{UserID: in.ChatterUserID, UserLogin: in.ChatterUserLogin, UserName: in.ChatterUserName}.user()
	gifter := in.gifter()
	event.Gifter = gifter.user()
	event.Anonymous = gifter != nil && gifter.IsAnonymous
	if in.PrimePaidUpgrade != nil {
		event.Prime, event.Tier = true, in.PrimePaidUpgrade.Tier
	}
	return event
}
