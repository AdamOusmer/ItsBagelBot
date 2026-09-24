// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"fmt"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

// Must stay just under the gossip providers' 15s handlerTimeout.
const gossipRPCTimeout = 12 * time.Second

// Must stay just above gossip custom.fetch's 3s fetchTimeout.
const customFetchRPCTimeout = 3500 * time.Millisecond

const (
	customFetchProvider = "custom"
	customFetchEndpoint = "fetch"
)

type UrlFetchCaller interface {
	Fetch(ctx context.Context, req gossiprpc.Request) (gossiprpc.CustomFetchReply, error)
}

type GossipRoute struct {
	Provider string
	Endpoint string
}

type GossipCaller interface {
	Call(ctx context.Context, route GossipRoute, req gossiprpc.Request, out any) error
}

type GossipRPC struct {
	nc     *nats.Conn
	prefix string
}

func NewGossipRPC(nc *nats.Conn, prefix string) *GossipRPC {
	return &GossipRPC{nc: nc, prefix: prefix}
}

func (g *GossipRPC) Call(ctx context.Context, route GossipRoute, req gossiprpc.Request, out any) error {
	subject := gossiprpc.Subject(g.prefix, route.Provider, route.Endpoint)

	ctx, cancel := context.WithTimeout(ctx, gossipRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(req)
	if err != nil {
		return fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}
	msg, err := bus.RequestWithContext(ctx, g.nc, subject, body)
	if err != nil {
		return fmt.Errorf("rpc %s request: %w", subject, err)
	}

	if message := bus.ReplyErrorMessage(msg.Data); message != "" {
		return bus.RPCReplyError{Subject: subject, Message: message}
	}
	if err := codec.Unmarshal(msg.Data, out); err != nil {
		return fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	return nil
}

func (g *GossipRPC) Fetch(ctx context.Context, req gossiprpc.Request) (gossiprpc.CustomFetchReply, error) {
	subject := gossiprpc.Subject(g.prefix, customFetchProvider, customFetchEndpoint)

	ctx, cancel := context.WithTimeout(ctx, customFetchRPCTimeout)
	defer cancel()

	body, err := codec.Marshal(req)
	if err != nil {
		return gossiprpc.CustomFetchReply{}, fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}
	msg, err := bus.RequestWithContext(ctx, g.nc, subject, body)
	if err != nil {
		return gossiprpc.CustomFetchReply{}, fmt.Errorf("rpc %s request: %w", subject, err)
	}

	if message := bus.ReplyErrorMessage(msg.Data); message != "" {
		return gossiprpc.CustomFetchReply{}, bus.RPCReplyError{Subject: subject, Message: message}
	}
	var reply gossiprpc.CustomFetchReply
	if err := codec.Unmarshal(msg.Data, &reply); err != nil {
		return gossiprpc.CustomFetchReply{}, fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	return reply, nil
}
