package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const gatewayRPCTimeout = 8 * time.Second

// GatewayCaller is the external-data surface modules pull third-party stats
// through (the urchin and mcsr modules). One generic Call keeps Deps flat while
// each module keeps its replies typed: it passes the gatewayrpc reply struct it
// expects as out.
type GatewayCaller interface {
	Call(ctx context.Context, provider, endpoint string, req gatewayrpc.Request, out any) error
}

// GatewayRPC implements GatewayCaller over NATS request/reply against the
// gateway service.
type GatewayRPC struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.gateway"
}

// NewGatewayRPC returns a GatewayCaller for the gateway service. prefix is the
// subject prefix the gateway subscribes under (default "bagel.rpc.gateway").
func NewGatewayRPC(nc *nats.Conn, prefix string) *GatewayRPC {
	return &GatewayRPC{nc: nc, prefix: prefix}
}

// Call requests one gateway endpoint and decodes the reply into out. A reply
// carrying the conventional {"error": "..."} envelope (player not found, ...)
// is returned as a bus.RPCReplyError so a module can chat the message back;
// any other failure (timeout, no responder) is an infrastructure error.
func (g *GatewayRPC) Call(ctx context.Context, provider, endpoint string, req gatewayrpc.Request, out any) error {
	subject := gatewayrpc.Subject(g.prefix, provider, endpoint)

	ctx, cancel := context.WithTimeout(ctx, gatewayRPCTimeout)
	defer cancel()

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("rpc %s marshal request: %w", subject, err)
	}
	msg, err := g.nc.RequestWithContext(ctx, subject, body)
	if err != nil {
		return fmt.Errorf("rpc %s request: %w", subject, err)
	}

	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &envelope); err == nil && envelope.Error != "" {
		return bus.RPCReplyError{Subject: subject, Message: envelope.Error}
	}
	if err := json.Unmarshal(msg.Data, out); err != nil {
		return fmt.Errorf("rpc %s unmarshal reply: %w", subject, err)
	}
	return nil
}
