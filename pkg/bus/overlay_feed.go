// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

// OverlayFeed publishes to OVERLAY_FEED over the connection of the overlays_feed_bus role.
type OverlayFeed struct {
	js jsapi.JetStream
}

func ConnectOverlayFeed(url string) (*nats.Conn, error) {
	identity := connectionIdentity{
		name:     "overlays-feed",
		jwt:      env.Get("OVERLAYS_FEED_BUS_JWT", ""),
		nkeySeed: env.Get("OVERLAYS_FEED_BUS_NKEY_SEED", ""),
	}
	if identity.jwt == "" || identity.nkeySeed == "" {
		return nil, errors.New("bus: missing overlay feed writer credentials")
	}
	return nats.Connect(busURL(endpoint(url)), baseOptions(identity)...)
}

func OpenOverlayFeed(ctx context.Context, nc *nats.Conn) (*OverlayFeed, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, err
	}
	if _, err := createOrGetStream(ctx, js, OverlayFeedStream); err != nil {
		return nil, err
	}
	return &OverlayFeed{js: js}, nil
}

// Publish stores payload under subject and returns its stream sequence; a repeated id inside the stream's duplicate window returns the first sequence.
func (f *OverlayFeed) Publish(ctx context.Context, subject, id string, payload []byte) (uint64, error) {
	segment := startMessagingSegment(ctx, messagingSpan{name: "nats.publish", operation: "publish", destination: subject})
	msg := nats.NewMsg(subject)
	msg.Data = payload
	insertQueueTraceHeaders(ctx, msg)
	ack, err := f.js.PublishMsg(ctx, msg, jsapi.WithMsgID(id), jsapi.WithExpectStream(OverlayFeedStream.Name))
	endMessagingSegment(segment, err)
	if err != nil {
		return 0, err
	}
	return ack.Sequence, nil
}
