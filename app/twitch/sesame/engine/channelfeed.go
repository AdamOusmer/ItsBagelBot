// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/channelevents"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	giftBundleTTL    = time.Minute
	giftBundleWindow = 5 * time.Second
)

// GiftSlot is the gifts of one tier in one channel; recipient events carry no link to their gift.
type GiftSlot struct {
	ChannelID string
	Tier      string
}

type GiftBundleStore interface {
	Open(ctx context.Context, slot GiftSlot, header pkg_valkey.GiftHeader) (pkg_valkey.GiftOpened, error)
	Add(ctx context.Context, slot GiftSlot, recipient []byte) (pkg_valkey.GiftAdded, error)
	Take(ctx context.Context, slot GiftSlot, bundleID string) (pkg_valkey.GiftTaken, bool, error)
}

type ValkeyGiftBundles struct {
	client valkey.Client
	bundle pkg_valkey.GiftBundle
}

func NewValkeyGiftBundles(client valkey.Client) *ValkeyGiftBundles {
	return &ValkeyGiftBundles{client: client, bundle: pkg_valkey.GiftBundle{TTL: giftBundleTTL, Window: giftBundleWindow}}
}

func giftKey(slot GiftSlot) string { return "feedgift:{" + slot.ChannelID + "}:" + slot.Tier }

func (s *ValkeyGiftBundles) Open(ctx context.Context, slot GiftSlot, header pkg_valkey.GiftHeader) (pkg_valkey.GiftOpened, error) {
	return s.bundle.Open(ctx, s.client, giftKey(slot), header)
}

func (s *ValkeyGiftBundles) Add(ctx context.Context, slot GiftSlot, recipient []byte) (pkg_valkey.GiftAdded, error) {
	return s.bundle.Add(ctx, s.client, giftKey(slot), recipient)
}

func (s *ValkeyGiftBundles) Take(ctx context.Context, slot GiftSlot, bundleID string) (pkg_valkey.GiftTaken, bool, error) {
	return s.bundle.Take(ctx, s.client, giftKey(slot), bundleID)
}

// PublishChannelEvent publishes with the event id as the message id so a redelivery cannot double it.
func PublishChannelEvent(ctx context.Context, pub bus.Publisher, event channelevents.Event) error {
	payload, err := codec.Marshal(event)
	if err != nil {
		return err
	}
	return bus.PublishConfirmed(ctx, pub, bus.Publication{
		Subject: channelevents.Subject(event.ChannelID, event.Kind), ID: event.ID, Payload: payload,
	})
}
