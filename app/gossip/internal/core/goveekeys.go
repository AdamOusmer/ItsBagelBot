// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	goveerpc "ItsBagelBot/internal/domain/rpc/govee"

	"github.com/nats-io/nats.go"
)

type GoveeKeyClient struct {
	get *KeyClient[goveerpc.KeyGetRequest, goveerpc.KeyGetReply]
}

func NewGoveeKeyClient(nc *nats.Conn, prefix string) *GoveeKeyClient {
	return &GoveeKeyClient{get: newKeyClient[goveerpc.KeyGetRequest](keyClientConfig[goveerpc.KeyGetReply]{
		NC:       nc,
		Subject:  prefix + ".get",
		Label:    "govee key get",
		ReplyErr: func(r goveerpc.KeyGetReply) string { return r.Error },
	})}
}

func (c *GoveeKeyClient) Key(ctx context.Context, broadcasterID string) (string, error) {
	reply, err := c.get.Call(ctx, goveerpc.KeyGetRequest{UserID: broadcasterID})
	if err != nil {
		return "", err
	}
	return reply.Key, nil
}
