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
	channelCountsRPCTimeout  = 3500 * time.Millisecond
	channelCountsPositiveTTL = 120 * time.Second
	channelCountsNegativeTTL = 30 * time.Second

	// channelCountsCacheCapacity mirrors uptimeCacheCapacity's reasoning: one
	// entry per enrolled broadcaster, so a small ceiling covers the fleet many
	// times over.
	channelCountsCacheCapacity int64 = 1024
)

// ChannelCountsResult is the resolved {followers}/{subs} read. Each half
// carries its own OK because the two ride different identities (the bot's
// for followers, the broadcaster's own for subs) and a grant can be missing
// one scope and not the other — see outgress's ChannelCountsReply.
type ChannelCountsResult struct {
	Followers   int
	FollowersOK bool
	Subs        int
	SubsOK      bool
}

type ChannelCountsLookup interface {
	Lookup(ctx context.Context, broadcasterID string) (ChannelCountsResult, error)
}

// ChannelCountsRPC is sesame's cached reader behind {followers}/{subs}, the
// same shape as UptimeRPC: outgress supplies the authenticated Twitch reads,
// freshness and cache policy live here. Both halves cache together — one
// outgress round trip answers both either way, so splitting the cache per
// half would only buy two TTL clocks to keep in sync.
type ChannelCountsRPC struct {
	cache   *cache.Cache[ChannelCountsResult]
	request func(context.Context, outgressrpc.ChannelCountsRequest) (outgressrpc.ChannelCountsReply, error)
}

func NewChannelCountsRPC(nc *nats.Conn, prefix string) *ChannelCountsRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".channelcounts.get"
	return &ChannelCountsRPC{
		cache: cache.New[ChannelCountsResult](channelCountsCacheCapacity, channelCountsNegativeTTL),
		request: func(ctx context.Context, req outgressrpc.ChannelCountsRequest) (outgressrpc.ChannelCountsReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.ChannelCountsReply](ctx, nc, subject, req, channelCountsRPCTimeout)
		},
	}
}

// Lookup resolves one broadcaster's counts, TTL'd positive only when BOTH
// halves answered and short-TTL'd otherwise — one cache entry backs both
// spans, so a single TTL has to cover the worse of the two: a followers-only
// answer that cached for the full positive window would pin the missing subs
// half as "cannot say" long after the broadcaster's grant could have
// answered it. The short TTL re-tries a degraded half soon instead.
func (r *ChannelCountsRPC) Lookup(ctx context.Context, broadcasterID string) (ChannelCountsResult, error) {
	return r.cache.GetOrLoadTTL(ctx, broadcasterID, func(ctx context.Context) (ChannelCountsResult, time.Duration, error) {
		reply, err := r.request(ctx, outgressrpc.ChannelCountsRequest{BroadcasterID: broadcasterID})
		if err != nil {
			return ChannelCountsResult{}, 0, err
		}
		if reply.Error != "" {
			return ChannelCountsResult{}, 0, errors.New(reply.Error)
		}
		result := ChannelCountsResult{
			Followers: reply.Followers, FollowersOK: reply.FollowersOK,
			Subs: reply.Subs, SubsOK: reply.SubsOK,
		}
		ttl := channelCountsNegativeTTL
		if result.FollowersOK && result.SubsOK {
			ttl = channelCountsPositiveTTL
		}
		return result, ttl, nil
	})
}
