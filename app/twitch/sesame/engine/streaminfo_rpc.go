// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/nats-io/nats.go"
)

const streamInfoCacheCapacity int64 = 4096

type StreamInfoResult struct {
	UserFound   bool
	Live        bool
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

type StreamInfoLookup interface {
	Lookup(ctx context.Context, broadcasterID, login string) (StreamInfoResult, error)
}

type StreamInfoRPC struct {
	rpc *cachedRPC[outgressrpc.StreamInfoRequest, outgressrpc.StreamInfoReply, StreamInfoResult]
}

func NewStreamInfoRPC(nc *nats.Conn, prefix string) *StreamInfoRPC {
	return &StreamInfoRPC{rpc: newBusRPC[outgressrpc.StreamInfoRequest](nc, rpcEndpoint{
		subject:  rpcSubject(prefix, "streaminfo"),
		timeout:  uptimeRPCTimeout,
		capacity: streamInfoCacheCapacity,
		missTTL:  uptimeOfflineTTL,
	}, decodeStreamInfo)}
}

func (s *StreamInfoRPC) Lookup(ctx context.Context, broadcasterID, login string) (StreamInfoResult, error) {
	if login == "" {
		return s.rpc.lookup(ctx, broadcasterID, outgressrpc.StreamInfoRequest{BroadcasterID: broadcasterID})
	}
	req := outgressrpc.StreamInfoRequest{CallerBroadcasterID: broadcasterID, TargetLogin: login}
	return s.rpc.lookup(ctx, broadcasterID+"/"+login, req)
}

func decodeStreamInfo(reply outgressrpc.StreamInfoReply) (StreamInfoResult, time.Duration, string) {
	result := StreamInfoResult{
		UserFound: reply.UserFound, Live: reply.Live, Title: reply.Title,
		GameName: reply.GameName, ViewerCount: reply.ViewerCount, StartedAt: reply.StartedAt,
	}
	return result, ttlWhen(reply.Live, uptimePositiveTTL, uptimeOfflineTTL), reply.Error
}
