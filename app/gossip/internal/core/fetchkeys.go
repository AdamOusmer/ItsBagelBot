// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"

	"github.com/nats-io/nats.go"
)

type FetchKeyClient struct {
	get *KeyClient[fetchkeyrpc.KeyGetRequest, fetchkeyrpc.KeyGetReply]
}

func NewFetchKeyClient(nc *nats.Conn, prefix string) *FetchKeyClient {
	return &FetchKeyClient{get: newKeyClient[fetchkeyrpc.KeyGetRequest](keyClientConfig[fetchkeyrpc.KeyGetReply]{
		NC:       nc,
		Subject:  prefix + ".get",
		Label:    "fetch key get",
		ReplyErr: func(r fetchkeyrpc.KeyGetReply) string { return r.Error },
	})}
}

func (c *FetchKeyClient) FetchKey(ctx context.Context, broadcasterID, label string) (string, error) {
	reply, err := c.get.Call(ctx, fetchkeyrpc.KeyGetRequest{UserID: broadcasterID, Label: label})
	if err != nil {
		return "", err
	}
	return reply.Key, nil
}
