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
	board(ctx context.Context, name string, limit int) ([]loyaltyrpc.CounterRank, bool)
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
	reply, ok := l.call(ctx, "counter.get", loyaltyrpc.Request{UserID: userID, Name: name})
	return foundValue(reply), ok
}

func foundValue(reply loyaltyrpc.Reply) int64 {
	if !reply.Found || reply.Counter == nil {
		return 0
	}
	return reply.Counter.Value
}

func (l *loyaltyCounters) board(ctx context.Context, name string, limit int) ([]loyaltyrpc.CounterRank, bool) {
	reply, ok := l.call(ctx, "counter.board", loyaltyrpc.Request{UserID: "0", Name: name, Limit: limit})
	return reply.Board, ok
}

func (l *loyaltyCounters) call(ctx context.Context, verb string, req loyaltyrpc.Request) (loyaltyrpc.Reply, bool) {
	ctx, cancel := context.WithTimeout(ctx, loyaltyCounterRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(req)
	if err != nil {
		return loyaltyrpc.Reply{}, false
	}
	msg, err := l.request(ctx, l.prefix+"."+verb, body)
	if err != nil {
		return loyaltyrpc.Reply{}, false
	}
	var reply loyaltyrpc.Reply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil || reply.Error != "" {
		return loyaltyrpc.Reply{}, false
	}
	return reply, true
}
