// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

type freezeChat struct {
	chatReplier
	store          engine.LoyaltyFreeze
	emit           module.Emit
	channel, actor uint64
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
		chat := freezeChat{chatReplier: newChatReplier(c), store: d.LoyaltyFreeze, emit: emit, channel: c.BroadcasterID, actor: actor}
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
	view, err := f.store.FreezeSet(ctx, f.channel, minutes, f.actor)
	if err != nil {
		return err
	}
	at := time.UnixMilli(view.FrozenAsOf).UTC().Format("15:04")
	f.reply(f.emit, "", "loyalty.freeze.started", "time", at)
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
