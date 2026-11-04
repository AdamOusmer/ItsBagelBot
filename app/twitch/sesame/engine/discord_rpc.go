// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
)

// Must stay just above the Discord outgress discord.post handler timeout.
const discordPostRPCTimeout = 4500 * time.Millisecond

type DiscordPoster interface {
	Post(ctx context.Context, req outgressrpc.DiscordPostRequest) (outgressrpc.DiscordPostReply, error)
}

type DiscordRPC struct {
	nc      *nats.Conn
	subject string
}

func NewDiscordRPC(nc *nats.Conn, prefix string) *DiscordRPC {
	return &DiscordRPC{nc: nc, subject: strings.TrimSuffix(prefix, ".") + ".discord.post"}
}

func (d *DiscordRPC) Post(ctx context.Context, req outgressrpc.DiscordPostRequest) (outgressrpc.DiscordPostReply, error) {
	return bus.RequestJSONTimeout[outgressrpc.DiscordPostReply](ctx, d.nc, d.subject, req, discordPostRPCTimeout)
}
