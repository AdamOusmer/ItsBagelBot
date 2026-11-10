// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/nats-io/nats.go"
)

const (
	uptimeRPCTimeout  = 3500 * time.Millisecond
	uptimePositiveTTL = time.Minute
	uptimeOfflineTTL  = 30 * time.Second

	uptimeCacheCapacity int64 = 1024
)

type UptimeResult struct {
	Live      bool
	StartedAt time.Time
}

type UptimeLookup interface {
	Lookup(ctx context.Context, broadcasterID string) (UptimeResult, error)
}

type UptimeRPC struct {
	rpc *cachedRPC[outgressrpc.UptimeRequest, outgressrpc.UptimeReply, UptimeResult]
}

func NewUptimeRPC(nc *nats.Conn, prefix string) *UptimeRPC {
	return &UptimeRPC{rpc: newBusRPC[outgressrpc.UptimeRequest](nc, rpcEndpoint{
		subject:  rpcSubject(prefix, "uptime"),
		timeout:  uptimeRPCTimeout,
		capacity: uptimeCacheCapacity,
		missTTL:  uptimeOfflineTTL,
	}, decodeUptime)}
}

func decodeUptime(reply outgressrpc.UptimeReply) (UptimeResult, time.Duration, string) {
	return UptimeResult{Live: reply.Live, StartedAt: reply.StartedAt}, ttlWhen(reply.Live, uptimePositiveTTL, uptimeOfflineTTL), reply.Error
}

func (u *UptimeRPC) Lookup(ctx context.Context, broadcasterID string) (UptimeResult, error) {
	return u.rpc.lookup(ctx, broadcasterID, outgressrpc.UptimeRequest{BroadcasterID: broadcasterID})
}
