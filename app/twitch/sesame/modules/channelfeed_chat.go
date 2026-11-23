// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
)

// ChannelFeedChat publishes chat lines of premium channels on core NATS, where nothing is kept or replayed.
// It is separate from ChannelFeed so chat does not pay for replay protection.
func ChannelFeedChat(d engine.Deps) module.Module {
	m := module.NewModule("", module.KindCore)
	if d.ChatFeed == nil {
		return m.Build()
	}
	return m.On("channel.chat.message", onFeedChat(d)).Build()
}

func onFeedChat(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		if !c.Regress.IsPremium() || c.Env.ChatterUserID == "" {
			return nil
		}
		event, ok := newFeedEvent(c, channelevents.Chat)
		if !ok {
			return nil
		}
		event.User = &channelevents.User{ID: c.Env.ChatterUserID, Login: c.Env.ChatterUserLogin, Name: c.Env.ChatterUserName}
		event.Message = &channelevents.Message{Text: c.Env.Text, Fragments: fragments(c.Env.Text, ingressSpans(c.Env.Emotes))}
		event.Badges = feedBadges(c.Env.Badges)
		return d.ChatFeed.PublishChat(ctx, event)
	}
}
