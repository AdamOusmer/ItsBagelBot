// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
)

const (
	uptimeRPCTimeout  = 3500 * time.Millisecond
	uptimePositiveTTL = time.Minute
	uptimeOfflineTTL  = 30 * time.Second

	// uptimeCacheCapacity ceilings the uptime cache. It is keyed per
	// broadcaster only -- one entry per enrolled channel, not per viewer --
	// so a small ceiling covers the fleet many times over.
	uptimeCacheCapacity int64 = 1024
)

type UptimeResult struct {
	Live      bool
	StartedAt time.Time
}

type UptimeLookup interface {
	Lookup(ctx context.Context, broadcasterID string) (UptimeResult, error)
}

// UptimeRPC is Sesame's cached stream-uptime reader. Outgress supplies only the
// authenticated Twitch read; command freshness, singleflight and cache policy
// live here with the command runtime.
type UptimeRPC struct {
	cache   *cache.Cache[UptimeResult]
	request func(context.Context, outgressrpc.UptimeRequest) (outgressrpc.UptimeReply, error)
}

func NewUptimeRPC(nc *nats.Conn, prefix string) *UptimeRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".uptime.get"
	return &UptimeRPC{
		cache: cache.New[UptimeResult](uptimeCacheCapacity, uptimeOfflineTTL),
		request: func(ctx context.Context, req outgressrpc.UptimeRequest) (outgressrpc.UptimeReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.UptimeReply](ctx, nc, subject, req, uptimeRPCTimeout)
		},
	}
}

func (u *UptimeRPC) Lookup(ctx context.Context, broadcasterID string) (UptimeResult, error) {
	return u.cache.GetOrLoadTTL(ctx, broadcasterID, func(ctx context.Context) (UptimeResult, time.Duration, error) {
		reply, err := u.request(ctx, outgressrpc.UptimeRequest{BroadcasterID: broadcasterID})
		if err != nil {
			return UptimeResult{}, 0, err
		}
		if reply.Error != "" {
			return UptimeResult{}, 0, errors.New(reply.Error)
		}
		result := UptimeResult{Live: reply.Live, StartedAt: reply.StartedAt}
		ttl := uptimeOfflineTTL
		if result.Live {
			ttl = uptimePositiveTTL
		}
		return result, ttl, nil
	})
}
