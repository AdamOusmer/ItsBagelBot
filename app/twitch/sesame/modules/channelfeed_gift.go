// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

// giftFlushDelay is how long a gift waits for its recipient subscribes before publishing with those that arrived.
var giftFlushDelay = 2 * time.Second

type feedGift struct {
	feedIdentity
	IsAnonymous     bool   `json:"is_anonymous"`
	Total           int64  `json:"total"`
	Tier            string `json:"tier"`
	CumulativeTotal int64  `json:"cumulative_total"`
}

func buildGift(event channelevents.Event, in feedGift) channelevents.Event {
	event.Amount, event.Tier = in.Total, in.Tier
	event.Anonymous, event.CumulativeTotal = in.IsAnonymous, in.CumulativeTotal
	if !in.IsAnonymous {
		event.User = in.user()
	}
	return event
}

// onFeedGift opens the bundle that the is_gift subscribes of this gift fill. One channel event leaves per gift.
func onFeedGift(d engine.Deps) module.EventHandler {
	return func(ctx context.Context, c *module.Context, _ module.Emit) error {
		base, ok := newFeedEvent(c, channelevents.CommunityGift)
		if !ok {
			return nil
		}
		var in feedGift
		if err := codec.Unmarshal(c.Env.Event, &in); err != nil {
			return err
		}
		event := buildGift(base, in)
		payload, err := codec.Marshal(event)
		if err != nil {
			return err
		}
		slot := engine.GiftSlot{ChannelID: event.ChannelID, Tier: event.Tier}
		opened, err := d.GiftBundles.Open(ctx, slot, pkg_valkey.GiftHeader{ID: event.ID, Total: int(in.Total), Payload: payload})
		if err != nil {
			return err
		}
		if opened == pkg_valkey.GiftComplete {
			return flushGift(ctx, d, slot, event.ID)
		}
		time.AfterFunc(giftFlushDelay, idempotency.Async(ctx, func(ctx context.Context) error {
			return flushGift(ctx, d, slot, event.ID)
		}))
		return nil
	}
}

// addGiftRecipient never publishes the subscribe itself; it only completes the gift it belongs to.
func addGiftRecipient(ctx context.Context, d engine.Deps, c *module.Context, in feedSubscribe) error {
	if c.Env.Origin == "trial" {
		return nil
	}
	recipient, err := codec.Marshal(in.user())
	if err != nil {
		return err
	}
	slot := engine.GiftSlot{ChannelID: channelID(c), Tier: in.Tier}
	added, err := d.GiftBundles.Add(ctx, slot, recipient)
	if err != nil || !added.Complete {
		return err
	}
	return flushGift(ctx, d, slot, added.BundleID)
}

func flushGift(ctx context.Context, d engine.Deps, slot engine.GiftSlot, bundleID string) error {
	taken, found, err := d.GiftBundles.Take(ctx, slot, bundleID)
	if err != nil || !found {
		return err
	}
	var event channelevents.Event
	if err := codec.Unmarshal(taken.Header, &event); err != nil {
		return err
	}
	for _, raw := range taken.Recipients {
		var recipient channelevents.User
		if err := codec.Unmarshal(raw, &recipient); err != nil {
			return err
		}
		event.Recipients = append(event.Recipients, recipient)
	}
	return engine.PublishChannelEvent(ctx, d.Pub, event)
}
