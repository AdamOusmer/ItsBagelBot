// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

// loyaltyCounterRPCTimeout bounds one counter.get round trip. The go-live
// snapshot (the only caller) fires once per stream, not on a hot path, so
// this favors giving loyalty room over failing fast.
const loyaltyCounterRPCTimeout = 2 * time.Second

// loyaltyCounterReader is the narrow surface Projector.snapshotCounterBaseline
// needs from loyalty. *loyaltyCounters satisfies it in production; tests can
// supply a fake instead of standing up a NATS connection.
type loyaltyCounterReader interface {
	get(ctx context.Context, userID, name string) (int64, bool)
}

// loyaltyCounters is the projector's narrow read-only client onto the loyalty
// service's counter store (<prefix>.counter.get — see app/loyalty/rpc/rpc.go's
// Subscribe doc), used only to seed a per-stream counter baseline at go-live
// (Projector.snapshotCounterBaseline). It deliberately does not grow into the
// fuller client sesame's engine.LoyaltyRPC already is: the projector only
// ever reads this one verb.
type loyaltyCounters struct {
	request func(ctx context.Context, subject string, data []byte) (*nats.Msg, error)
	prefix  string
}

// newLoyaltyCounters returns a reader bound to nc under prefix (e.g.
// "bagel.rpc.loyalty").
func newLoyaltyCounters(nc *nats.Conn, prefix string) *loyaltyCounters {
	return &loyaltyCounters{
		request: func(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, data)
		},
		prefix: prefix,
	}
}

// get reads one channel-scope counter's current lifetime value. ok is false
// only when the round trip itself failed (marshal, timeout, transport, an
// error-carrying reply); a counter loyalty has never created is NOT a
// failure — it comes back found:false, which this treats as an honest 0
// (same precedent as the dashboard's public-stats.ts counterValue doc: "an
// absent counter is honestly 0").
func (l *loyaltyCounters) get(ctx context.Context, userID, name string) (int64, bool) {
	subject := l.prefix + ".counter.get"
	ctx, cancel := context.WithTimeout(ctx, loyaltyCounterRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(loyaltyrpc.Request{UserID: userID, Name: name})
	if err != nil {
		return 0, false
	}
	msg, err := l.request(ctx, subject, body)
	if err != nil {
		return 0, false
	}
	var reply loyaltyrpc.Reply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil || reply.Error != "" {
		return 0, false
	}
	if !reply.Found || reply.Counter == nil {
		return 0, true
	}
	return reply.Counter.Value, true
}
