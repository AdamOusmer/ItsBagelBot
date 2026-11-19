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
	store engine.LoyaltyFreeze
	emit  module.Emit
}

func freezeRun(d engine.Deps, release bool) module.RunFunc {
	return func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
		if d.LoyaltyFreeze == nil {
			return nil
		}
		chat := freezeChat{chatReplier: newChatReplier(c), store: d.LoyaltyFreeze, emit: emit}
		ref := engine.EffectRef{Identity: engine.EventIdentity(&c.Env), Effect: engine.EffectLoyaltyFreeze}
		return d.Dedup.Effect(ctx, ref, func(ctx context.Context) error {
			command := engine.FreezeCommand{BroadcasterID: c.BroadcasterID, Holder: loyaltyrpc.FreezeModHolder,
				BlockTransfer: loyaltyrpc.FreezeDefaultBlockTransfer, CloseGames: loyaltyrpc.FreezeDefaultCloseGames}
			if release {
				return chat.release(ctx, command)
			}
			return chat.set(ctx, command, args)
		})
	}
}

func (f freezeChat) set(ctx context.Context, command engine.FreezeCommand, args string) error {
	minutes, ok := freezeMinutes(args)
	if !ok {
		f.reply(f.emit, "", "loyalty.freeze.usage")
		return nil
	}
	command.Minutes = minutes
	view, changed, err := f.store.FreezeSet(ctx, command)
	if err != nil {
		return err
	}
	key := replyKey("loyalty.freeze.active")
	if changed {
		key = "loyalty.freeze.started"
	}
	left := time.Until(time.UnixMilli(view.ExpiresAt))
	f.reply(f.emit, "", key, "minutes", strconv.Itoa(int(max(0, (left+time.Minute-1)/time.Minute))))
	return nil
}

func freezeMinutes(args string) (int, bool) {
	if args == "" {
		return loyaltyrpc.FreezeDefaultMinutes, true
	}
	minutes, err := strconv.Atoi(args)
	return minutes, err == nil && minutes >= 1 && minutes <= int(loyaltyrpc.FreezeMaximum/time.Minute)
}

func (f freezeChat) release(ctx context.Context, command engine.FreezeCommand) error {
	_, changed, err := f.store.FreezeRelease(ctx, command)
	key := replyKey("loyalty.unfreeze.none")
	switch {
	case engine.LoyaltyFreezeRefusal(err, loyaltyrpc.RefusalHeldByRound):
		key = "loyalty.unfreeze.round"
	case err != nil:
		return err
	case changed:
		key = "loyalty.unfreeze.done"
	}
	f.reply(f.emit, "", key)
	return nil
}
