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

const loyaltyCounterRPCTimeout = 2 * time.Second

type loyaltyCounterReader interface {
	get(ctx context.Context, userID, name string) (int64, bool)
}

type loyaltyCounters struct {
	request func(ctx context.Context, subject string, data []byte) (*nats.Msg, error)
	prefix  string
}

func newLoyaltyCounters(nc *nats.Conn, prefix string) *loyaltyCounters {
	return &loyaltyCounters{
		request: func(ctx context.Context, subject string, data []byte) (*nats.Msg, error) {
			return bus.RequestWithContext(ctx, nc, subject, data)
		},
		prefix: prefix,
	}
}

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
