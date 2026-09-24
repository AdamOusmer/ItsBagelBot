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

	channelCountsCacheCapacity int64 = 1024
)

type ChannelCountsResult struct {
	Followers   int
	FollowersOK bool
	Subs        int
	SubsOK      bool
}

type ChannelCountsLookup interface {
	Lookup(ctx context.Context, broadcasterID string) (ChannelCountsResult, error)
}

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
