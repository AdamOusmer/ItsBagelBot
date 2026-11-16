// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

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
	nc        *nats.Conn
	prefix    string
	analytics *analytics.Recorder
}

func NewGossipRPC(nc *nats.Conn, prefix string) *GossipRPC {
	return &GossipRPC{nc: nc, prefix: prefix}
}

func (g *GossipRPC) SetAnalytics(rec *analytics.Recorder) { g.analytics = rec }

func (g *GossipRPC) Call(ctx context.Context, route GossipRoute, req gossiprpc.Request, out any) error {
	ctx, cancel := context.WithTimeout(ctx, gossipRPCTimeout)
	defer cancel()
	err := bus.RequestInto(ctx, g.nc, bus.RPCRequest{Subject: gossiprpc.Subject(g.prefix, route.Provider, route.Endpoint), Body: req}, out)
	g.analytics.Count(catalog.ProviderCalls, route.Provider, providerFetchStatus(out, err))
	return err
}

func providerFetchStatus(out any, err error) string {
	if err != nil {
		return "error"
	}
	if reply, ok := out.(*gossiprpc.CustomFetchReply); ok {
		return string(reply.Status)
	}
	return "ok"
}

func (g *GossipRPC) Fetch(ctx context.Context, req gossiprpc.Request) (gossiprpc.CustomFetchReply, error) {
	ctx, cancel := context.WithTimeout(ctx, customFetchRPCTimeout)
	defer cancel()
	var reply gossiprpc.CustomFetchReply
	err := g.Call(ctx, GossipRoute{Provider: customFetchProvider, Endpoint: customFetchEndpoint}, req, &reply)
	return reply, err
}
