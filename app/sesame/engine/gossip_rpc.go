package engine

import (
	"context"
	"fmt"
	"time"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
)

// gossipRPCTimeout bounds one gossip request from sesame's side. It sits just
// under the gossip's own 15s handler budget: a cold lookup (a player the
// upstream has never tracked forces a synchronous Mojang/Hypixel resolve) can
// take 5-10s, and giving up earlier made the first command per cold player die
// silently while the gossip finished and cached — the "works on the second
// try" symptom.
const gossipRPCTimeout = 12 * time.Second

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

	body, err := sonic.Marshal(req)
	if err != nil {
		return fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}
	msg, err := bus.RequestWithContext(ctx, g.nc, subject, body)
	if err != nil {
		return fmt.Errorf("rpc %s request: %w", subject, err)
	}

	var envelope struct {
		Error string `json:"error"`
	}
	if err := sonic.Unmarshal(msg.Data, &envelope); err == nil && envelope.Error != "" {
		return bus.RPCReplyError{Subject: subject, Message: envelope.Error}
	}
	if err := sonic.Unmarshal(msg.Data, out); err != nil {
		return fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	return nil
}
