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
	followageRPCTimeout  = 3500 * time.Millisecond
	followagePositiveTTL = 15 * time.Minute
	followageNegativeTTL = time.Minute

	followageCacheCapacity int64 = 8192
)

type FollowageResult struct {
	TargetID   string
	UserFound  bool
	Following  bool
	FollowedAt time.Time
}

type FollowageQuery struct {
	BroadcasterID string
	TargetID      string
	TargetLogin   string
	Trial         bool
}

type FollowageLookup interface {
	Lookup(ctx context.Context, query FollowageQuery) (FollowageResult, error)
}

type FollowageRPC struct {
	rpc *cachedRPC[outgressrpc.FollowageRequest, outgressrpc.FollowageReply, FollowageResult]
}

func NewFollowageRPC(nc *nats.Conn, prefix string) *FollowageRPC {
	return &FollowageRPC{rpc: newBusRPC[outgressrpc.FollowageRequest](nc, rpcEndpoint{
		subject:  rpcSubject(prefix, "followage"),
		timeout:  followageRPCTimeout,
		capacity: followageCacheCapacity,
		missTTL:  followageNegativeTTL,
	}, decodeFollowage)}
}

func decodeFollowage(reply outgressrpc.FollowageReply) (FollowageResult, time.Duration, string) {
	result := FollowageResult{
		TargetID: reply.TargetID, UserFound: reply.UserFound,
		Following: reply.Following, FollowedAt: reply.FollowedAt,
	}
	return result, ttlWhen(result.Following, followagePositiveTTL, followageNegativeTTL), reply.Error
}

func (f *FollowageRPC) Lookup(ctx context.Context, query FollowageQuery) (FollowageResult, error) {
	key := query.BroadcasterID + ":" + targetCacheKey(query.TargetID, query.TargetLogin)
	return f.rpc.lookup(ctx, key, outgressrpc.FollowageRequest{
		BroadcasterID: query.BroadcasterID, TargetID: query.TargetID, TargetLogin: query.TargetLogin, Trial: query.Trial,
	})
}
