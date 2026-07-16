package engine

import (
	"context"
	"errors"
	"time"

	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

const personalityRPCTimeout = 2 * time.Second

// PersonalityRPC implements FeedTotalBumper by forwarding to the modules
// service's personality RPC over NATS request/reply. The permanent counter
// row lives with the modules service; sesame bumps it through
// bagel.rpc.modules.personality.feed.
type PersonalityRPC struct {
	nc      *nats.Conn
	subject string // e.g. "bagel.rpc.modules.personality.feed"
}

// NewPersonalityRPC returns a FeedTotalBumper backed by the modules
// personality RPC. prefix is the modules RPC subject prefix (default
// "bagel.rpc.modules"); the client appends ".personality.feed".
func NewPersonalityRPC(nc *nats.Conn, modulesPrefix string) *PersonalityRPC {
	return &PersonalityRPC{nc: nc, subject: modulesPrefix + ".personality.feed"}
}

// FeedBump increments the permanent global feed counter and returns the new
// lifetime total.
func (c *PersonalityRPC) FeedBump(ctx context.Context) (uint64, error) {
	reply, err := bus.RequestJSONTimeout[modulesrpc.FeedBumpReply](ctx, c.nc, c.subject, modulesrpc.FeedBumpRequest{}, personalityRPCTimeout)
	if err != nil {
		return 0, err
	}
	if reply.Error != "" {
		return 0, errors.New(reply.Error)
	}
	return reply.Total, nil
}
