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
	rpc *cachedRPC[outgressrpc.ChannelCountsRequest, outgressrpc.ChannelCountsReply, ChannelCountsResult]
}

func NewChannelCountsRPC(nc *nats.Conn, prefix string) *ChannelCountsRPC {
	return &ChannelCountsRPC{rpc: newBusRPC[outgressrpc.ChannelCountsRequest](nc, rpcEndpoint{
		subject:  rpcSubject(prefix, "channelcounts"),
		timeout:  channelCountsRPCTimeout,
		capacity: channelCountsCacheCapacity,
		missTTL:  channelCountsNegativeTTL,
	}, decodeChannelCounts)}
}

func decodeChannelCounts(reply outgressrpc.ChannelCountsReply) (ChannelCountsResult, time.Duration, string) {
	result := ChannelCountsResult{
		Followers: reply.Followers, FollowersOK: reply.FollowersOK,
		Subs: reply.Subs, SubsOK: reply.SubsOK,
	}
	return result, ttlWhen(result.FollowersOK && result.SubsOK, channelCountsPositiveTTL, channelCountsNegativeTTL), reply.Error
}

func (r *ChannelCountsRPC) Lookup(ctx context.Context, broadcasterID string) (ChannelCountsResult, error) {
	return r.rpc.lookup(ctx, broadcasterID, outgressrpc.ChannelCountsRequest{BroadcasterID: broadcasterID})
}
