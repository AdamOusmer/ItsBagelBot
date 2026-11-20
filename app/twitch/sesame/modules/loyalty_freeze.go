// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

type freezeChat struct {
	chatReplier
	store   engine.LoyaltyFreeze
	emit    module.Emit
	channel draw.ChannelID
	actor   draw.ViewerID
}

func freezeRun(d engine.Deps, release bool) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.LoyaltyFreeze == nil {
			return nil
		}
		actor, err := strconv.ParseUint(c.Env.ChatterUserID, 10, 64)
		if err != nil {
			return err
		}
		chat := freezeChat{chatReplier: newChatReplier(c), store: d.LoyaltyFreeze, emit: emit,
			channel: draw.ChannelID(c.BroadcasterID), actor: draw.ViewerID(strconv.FormatUint(actor, 10))}
		ref := engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: engine.EffectLoyaltyFreeze}
		return d.Dedup.Effect(ctx, ref, func(ctx context.Context) error {
			if release {
				return chat.release(ctx)
			}
			return chat.set(ctx, args)
		})
	}
}

func (f freezeChat) set(ctx context.Context, args string) error {
	minutes, ok := freezeMinutes(args)
	if !ok {
		f.reply(f.emit, "", "loyalty.freeze.usage")
		return nil
	}
	_, err := f.store.FreezeSet(ctx, f.channel, engine.FreezeMinutes(minutes), f.actor)
	if err != nil {
		return err
	}
	f.reply(f.emit, "", "loyalty.freeze.started", "minutes", strconv.Itoa(minutes))
	return nil
}

func freezeMinutes(args string) (int, bool) {
	if args == "" {
		return loyaltyrpc.FreezeDefaultMinutes, true
	}
	minutes, err := strconv.Atoi(args)
	return minutes, err == nil && loyaltyrpc.ValidFreezeMinutes(minutes)
}

func (f freezeChat) release(ctx context.Context) error {
	released, err := f.store.FreezeRelease(ctx, f.channel, f.actor)
	if err != nil {
		return err
	}
	key := replyKey("loyalty.unfreeze.none")
	if released {
		key = "loyalty.unfreeze.done"
	}
	f.reply(f.emit, "", key)
	return nil
}
