// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	goveerpc "ItsBagelBot/internal/domain/rpc/govee"

	"github.com/nats-io/nats.go"
)

// GoveeKeyClient resolves a broadcaster's decrypted Govee API key over the
// modules service's internal RPC, gossip's twin of outgress's tokenstore.
// The plaintext key it returns is used for one upstream call and never cached.
type GoveeKeyClient struct {
	get *KeyClient[goveerpc.KeyGetRequest, goveerpc.KeyGetReply]
}

// NewGoveeKeyClient builds the resolver against the modules internal key RPC.
// prefix is e.g. "bagel.rpc.internal.govee.key".
func NewGoveeKeyClient(nc *nats.Conn, prefix string) *GoveeKeyClient {
	return &GoveeKeyClient{get: newKeyClient[goveerpc.KeyGetRequest](keyClientConfig[goveerpc.KeyGetReply]{
		NC:       nc,
		Subject:  prefix + ".get",
		Label:    "govee key get",
		ReplyErr: func(r goveerpc.KeyGetReply) string { return r.Error },
	})}
}

// Key returns the broadcaster's decrypted Govee API key, or "" (nil error) when
// none is on file. A transport or service failure is returned as an error.
func (c *GoveeKeyClient) Key(ctx context.Context, broadcasterID string) (string, error) {
	reply, err := c.get.Call(ctx, goveerpc.KeyGetRequest{UserID: broadcasterID})
	if err != nil {
		return "", err
	}
	return reply.Key, nil
}
