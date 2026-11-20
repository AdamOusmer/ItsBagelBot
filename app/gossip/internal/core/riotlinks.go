// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	riotrpc "ItsBagelBot/internal/domain/rpc/riot"

	"github.com/nats-io/nats.go"
)

type RiotLinkClient struct {
	get *KeyClient[riotrpc.LinkUserRequest, riotrpc.LinkReply]
}

func NewRiotLinkClient(nc *nats.Conn, prefix string) *RiotLinkClient {
	return &RiotLinkClient{
		get: newKeyClient[riotrpc.LinkUserRequest](keyClientConfig[riotrpc.LinkReply]{
			NC:       nc,
			Subject:  prefix + ".get",
			Label:    "riot link get",
			ReplyErr: func(r riotrpc.LinkReply) string { return r.Error },
		}),
	}
}

func (c *RiotLinkClient) RiotLink(ctx context.Context, broadcasterID string) (riotrpc.LinkReply, error) {
	return c.get.Call(ctx, riotrpc.LinkUserRequest{UserID: broadcasterID})
}
