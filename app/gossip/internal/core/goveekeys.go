package core

import (
	"context"
	"fmt"
	"time"

	goveerpc "ItsBagelBot/internal/domain/rpc/govee"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// goveeKeyTimeout bounds the internal key lookup. It is short: the modules
// service answers from its own database with no upstream hop, and a redemption
// the color reward acts on has its own generous handler budget on top.
const goveeKeyTimeout = 2 * time.Second

// GoveeKeyClient resolves a broadcaster's decrypted Govee API key over the
// modules service's internal RPC, the gateway's twin of outgress's tokenstore.
// The plaintext key it returns is used for one upstream call and never cached.
type GoveeKeyClient struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.internal.govee.key"
}

// NewGoveeKeyClient builds the resolver against the modules internal key RPC.
func NewGoveeKeyClient(nc *nats.Conn, prefix string) *GoveeKeyClient {
	return &GoveeKeyClient{nc: nc, prefix: prefix}
}

// Key returns the broadcaster's decrypted Govee API key, or "" (nil error) when
// none is on file. A transport or service failure is returned as an error.
func (c *GoveeKeyClient) Key(ctx context.Context, broadcasterID string) (string, error) {
	reply, err := bus.RequestJSONTimeout[goveerpc.KeyGetReply](
		ctx, c.nc, c.prefix+".get", goveerpc.KeyGetRequest{UserID: broadcasterID}, goveeKeyTimeout)
	if err != nil {
		return "", fmt.Errorf("govee key get rpc: %w", err)
	}
	if reply.Error != "" {
		return "", fmt.Errorf("govee key get: %s", reply.Error)
	}
	return reply.Key, nil
}
