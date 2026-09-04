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

// gossipRPCTimeout bounds one gossip request from sesame's side. It sits just
// under the gossip's own 15s handler budget: a cold lookup (a player the
// upstream has never tracked forces a synchronous Mojang/Hypixel resolve) can
// take 5-10s, and giving up earlier made the first command per cold player die
// silently while the gossip finished and cached — the "works on the second
// try" symptom.
const gossipRPCTimeout = 12 * time.Second

// customFetchRPCTimeout bounds one custom.fetch ({urlfetch:...} token) request.
// It is sized in the OPPOSITE direction of gossipRPCTimeout, for the same
// reason: this endpoint has no cold-resolve tail — its own budget is a fixed
// 3s (one SSRF-gated HTTP GET against an allow-listed URL plus a cache read,
// docs/urlfetch IMPLEMENTATION.md Phase 2) inside gossip's generic 5s handler
// default — so sitting just ABOVE the endpoint budget (3.5s) lets a slow
// upstream surface as gossip's typed timeout reply instead of dying as a
// client-side abort nobody can distinguish from a dead responder. Fetches run
// concurrently per response (fetchUrlTokens), so the overall chat-latency cap
// equals this figure, not a sum; nothing upstream bounds us — the ctx reaching
// runCustom carries no WithTimeout between delivery and expansion — so 3.5s
// is self-imposed and deliberately tight.
const customFetchRPCTimeout = 3500 * time.Millisecond

const (
	customFetchProvider = "custom"
	customFetchEndpoint = "fetch"
)

// UrlFetchCaller is the {urlfetch:...} token family's call surface — the
// custom.fetch twin of GossipCaller, narrowed to one endpoint's request/reply
// shape so fetchUrlTokens tests can stub it without a NATS connection (the
// fakeGossip pattern). The chat path sends Request{DefID, ChannelID,
// IsPremium} and leaves DryRun/Fresh/Def zero: dry-run/fresh are rehearsal
// knobs and inline defs never ride chat.
type UrlFetchCaller interface {
	Fetch(ctx context.Context, req gossiprpc.Request) (gossiprpc.CustomFetchReply, error)
}

// GossipRoute addresses one provider endpoint on the gossip service (e.g.
// provider "fortnite", endpoint "stats").
type GossipRoute struct {
	Provider string
	Endpoint string
}

// GossipCaller is the external-data surface modules pull third-party stats
// through (the urchin and mcsr modules). One generic Call keeps Deps flat while
// each module keeps its replies typed: it passes the gossiprpc reply struct it
// expects as out.
type GossipCaller interface {
	Call(ctx context.Context, route GossipRoute, req gossiprpc.Request, out any) error
}

// GossipRPC implements GossipCaller over NATS request/reply against the
// gossip service.
type GossipRPC struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.gossip"
}

// NewGossipRPC returns a GossipCaller for the gossip service. prefix is the
// subject prefix the gossip subscribes under (default "bagel.rpc.gossip").
func NewGossipRPC(nc *nats.Conn, prefix string) *GossipRPC {
	return &GossipRPC{nc: nc, prefix: prefix}
}

// Call requests one gossip endpoint and decodes the reply into out. A reply
// carrying the conventional {"error": "..."} envelope (player not found, ...)
// is returned as a bus.RPCReplyError so a module can chat the message back;
// any other failure (timeout, no responder) is an infrastructure error.
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

// Fetch requests one custom.fetch execution and decodes its typed reply. The
// skeleton mirrors Call (same marshal/request/envelope discipline), with two
// differences: the per-request deadline is customFetchRPCTimeout, and a reply
// carrying the conventional {"error": "..."} envelope still short-circuits as
// bus.RPCReplyError — an infrastructure-shaped answer the token mapper renders
// as the timeout-family fallback text. Endpoint failures ride Status instead,
// so no error is returned for them.
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
