// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"fmt"
	"time"

	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// fetchKeyTimeout bounds the internal fetch-key lookup. Same reasoning as
// goveeKeyTimeout: the commands service answers from its own row plus one
// Unpack with no upstream hop, and the caller's 3s endpoint budget carries
// the rest — the key resolve must never be the tail that eats it.
const fetchKeyTimeout = 2 * time.Second

// FetchKeyClient resolves a broadcaster's stored API key by label over the
// commands service's internal RPC — the custom urlfetch provider's twin of
// GoveeKeyClient. One call per fetch: the plaintext rides exactly one
// upstream call and is never cached, logged, or projected anywhere.
type FetchKeyClient struct {
	nc     *nats.Conn
	prefix string // e.g. "bagel.rpc.internal.commands.fetchkey"
}

// NewFetchKeyClient builds the resolver against the commands service's
// internal fetch-key RPC.
func NewFetchKeyClient(nc *nats.Conn, prefix string) *FetchKeyClient {
	return &FetchKeyClient{nc: nc, prefix: prefix}
}

// FetchKey returns the broadcaster's decrypted API key under label, or ""
// (nil error) when none is on file — which callers must treat as fail-closed,
// never as an unauthenticated request. A transport or service failure is an
// error; an Unpack/AAD mismatch surfaces through it as well and is logged
// upstream with user_id+label only.
func (c *FetchKeyClient) FetchKey(ctx context.Context, broadcasterID, label string) (string, error) {
	reply, err := bus.RequestJSONTimeout[fetchkeyrpc.KeyGetReply](
		ctx, c.nc, c.prefix+".get", fetchkeyrpc.KeyGetRequest{UserID: broadcasterID, Label: label}, fetchKeyTimeout)
	if err != nil {
		return "", fmt.Errorf("fetch key get rpc: %w", err)
	}
	if reply.Error != "" {
		return "", fmt.Errorf("fetch key get: %s", reply.Error)
	}
	return reply.Key, nil
}
