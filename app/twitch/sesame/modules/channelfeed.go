// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/pkg/codec"
)

type feedIdentity struct {
	UserID    string `json:"user_id"`
	UserLogin string `json:"user_login"`
	UserName  string `json:"user_name"`
}

func (i feedIdentity) user() *channelevents.User {
	if i == (feedIdentity{}) {
		return nil
	}
	return &channelevents.User{ID: i.UserID, Login: i.UserLogin, Name: i.UserName}
}

type feedEmote struct {
	ID    string `json:"id"`
	Begin int    `json:"begin"`
	End   int    `json:"end"`
}

type feedFollow struct{ feedIdentity }

type feedSubscribe struct {
	feedIdentity
	Tier   string `json:"tier"`
	IsGift bool   `json:"is_gift"`
}

type feedResub struct {
	feedIdentity
	Tier    string `json:"tier"`
	Months  int    `json:"cumulative_months"`
	Streak  int    `json:"streak_months"`
	Message struct {
		Text   string      `json:"text"`
		Emotes []feedEmote `json:"emotes"`
	} `json:"message"`
}

type feedCheer struct {
	feedIdentity
	IsAnonymous bool   `json:"is_anonymous"`
	Bits        int64  `json:"bits"`
	Message     string `json:"message"`
}

type feedRaid struct {
	UserID    string `json:"from_broadcaster_user_id"`
	UserLogin string `json:"from_broadcaster_user_login"`
	UserName  string `json:"from_broadcaster_user_name"`
	Viewers   int64  `json:"viewers"`
}

// ChannelFeed normalizes Twitch events onto CHANNEL_EVENTS. It is independent of the alerts module:
// no chat toggle, no viewer claims and no cooldowns.
func ChannelFeed(d engine.Deps) module.Module {
	m := module.NewModule("", module.KindCore).ProtectReplay()
	if d.Pub == nil || d.GiftBundles == nil {
		return m.Build()
	}
	return m.
		On("channel.follow", alphaOnly(d, feedHandler(d, channelevents.Follow, buildFollow))).
		On("channel.subscribe", alphaOnly(d, onFeedSubscribe(d))).
		On("channel.subscription.message", alphaOnly(d, feedHandler(d, channelevents.Resub, buildResub))).
		On("channel.subscription.gift", alphaOnly(d, onFeedGift(d))).
		On("channel.cheer", alphaOnly(d, feedHandler(d, channelevents.Cheer, buildCheer))).
		On("channel.raid", alphaOnly(d, feedHandler(d, channelevents.Raid, buildRaid))).
		On(engine.RedemptionAddType, alphaOnly(d, feedHandler(d, channelevents.Redemption, buildRedemption))).
		On("channel.chat.notification", alphaOnly(d, onFeedNotice(d))).
		Build()
}

// feedAlpha is false without a projection reader, so a non-alpha channel never reaches the feed path.
func feedAlpha(ctx context.Context, d engine.Deps, channel uint64) (bool, error) {
	if d.Proj == nil {
		return false, nil
	}
	user, err := d.Proj.User(ctx, channel)
	return user.IsAlpha(), err
}

func alphaOnly(d engine.Deps, next module.EventHandler) module.EventHandler {
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		alpha, err := feedAlpha(ctx, d, c.BroadcasterID)
		if err != nil || !alpha {
			return err
		}
		return next(ctx, c, emit)
	}
}

// newFeedEvent is false for trial traffic and for events with no Twitch identity to dedupe on.
func newFeedEvent(c *module.Context, kind channelevents.Kind) (channelevents.Event, bool) {
	id := engine.EventIdentity(&c.Env)
	if c.Env.Origin == "trial" || id == "" {
		return channelevents.Event{}, false
	}
	return channelevents.Event{
		ID: id, Kind: kind,
		ChannelID:  channelID(c),
		OccurredAt: time.UnixMilli(c.EventVersion()).UTC(),
	}, true
}

func channelID(c *module.Context) string { return strconv.FormatUint(c.BroadcasterID, 10) }

func feedHandler[T any](d engine.Deps, kind channelevents.Kind, build func(channelevents.Event, T) channelevents.Event) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		base, ok := newFeedEvent(c, kind)
		if !ok {
			return nil
		}
		var in T
		if err := codec.Unmarshal(c.Env.Event, &in); err != nil {
			return err
		}
		return engine.PublishChannelEvent(ctx, d.Pub, build(base, in))
	}
}

func buildFollow(event channelevents.Event, in feedFollow) channelevents.Event {
	event.User = in.user()
	return event
}

func onFeedSubscribe(d engine.Deps) module.EventHandler {
	announce := feedHandler(d, channelevents.Sub, buildSub)
	return func(ctx context.Context, c *module.Context, emit module.Emit) error {
		var in feedSubscribe
		if err := codec.Unmarshal(c.Env.Event, &in); err != nil {
			return err
		}
		if !in.IsGift {
			return announce(ctx, c, emit)
		}
		return addGiftRecipient(ctx, d, c, in)
	}
}

func buildSub(event channelevents.Event, in feedSubscribe) channelevents.Event {
	event.User, event.Tier = in.user(), in.Tier
	return event
}

func buildResub(event channelevents.Event, in feedResub) channelevents.Event {
	event.User, event.Tier = in.user(), in.Tier
	event.Months, event.Streak = in.Months, in.Streak
	if in.Message.Text != "" {
		event.Message = &channelevents.Message{Text: in.Message.Text, Fragments: fragments(in.Message.Text, inclusiveSpans(in.Message.Emotes))}
	}
	return event
}

func buildCheer(event channelevents.Event, in feedCheer) channelevents.Event {
	event.Amount, event.Anonymous = in.Bits, in.IsAnonymous
	if !in.IsAnonymous {
		event.User = in.user()
	}
	if in.Message != "" {
		event.Message = &channelevents.Message{Text: in.Message}
	}
	return event
}

func buildRaid(event channelevents.Event, in feedRaid) channelevents.Event {
	event.Amount = in.Viewers
	event.User = feedIdentity{UserID: in.UserID, UserLogin: in.UserLogin, UserName: in.UserName}.user()
	return event
}

func buildRedemption(event channelevents.Event, in engine.RedemptionEvent) channelevents.Event {
	event.User = feedIdentity{UserID: in.UserID, UserLogin: in.UserLogin, UserName: in.UserName}.user()
	event.Reward = &channelevents.Reward{ID: in.Reward.ID, Title: in.Reward.Title, Cost: int64(in.Reward.Cost), Input: in.UserInput}
	return event
}
