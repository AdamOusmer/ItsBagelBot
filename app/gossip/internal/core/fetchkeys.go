// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"

	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"

	"github.com/nats-io/nats.go"
)

// FetchKeyClient resolves a broadcaster's stored API key by label over the
// commands service's internal RPC — the custom urlfetch provider's twin of
// GoveeKeyClient. One call per fetch: the plaintext rides exactly one
// upstream call and is never cached, logged, or projected anywhere.
type FetchKeyClient struct {
	get *KeyClient[fetchkeyrpc.KeyGetRequest, fetchkeyrpc.KeyGetReply]
}

// NewFetchKeyClient builds the resolver against the commands service's
// internal fetch-key RPC. prefix is e.g.
// "bagel.rpc.internal.commands.fetchkey".
func NewFetchKeyClient(nc *nats.Conn, prefix string) *FetchKeyClient {
	return &FetchKeyClient{get: newKeyClient[fetchkeyrpc.KeyGetRequest](keyClientConfig[fetchkeyrpc.KeyGetReply]{
		NC:       nc,
		Subject:  prefix + ".get",
		Label:    "fetch key get",
		ReplyErr: func(r fetchkeyrpc.KeyGetReply) string { return r.Error },
	})}
}

// FetchKey returns the broadcaster's decrypted API key under label, or ""
// (nil error) when none is on file — which callers must treat as fail-closed,
// never as an unauthenticated request. A transport or service failure is an
// error; an Unpack/AAD mismatch surfaces through it as well and is logged
// upstream with user_id+label only.
func (c *FetchKeyClient) FetchKey(ctx context.Context, broadcasterID, label string) (string, error) {
	reply, err := c.get.Call(ctx, fetchkeyrpc.KeyGetRequest{UserID: broadcasterID, Label: label})
	if err != nil {
		return "", err
	}
	return reply.Key, nil
}
