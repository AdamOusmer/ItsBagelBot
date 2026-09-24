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
	cache   *cache.Cache[StreamInfoResult]
	request func(context.Context, outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error)
}

func NewStreamInfoRPC(nc *nats.Conn, prefix string) *StreamInfoRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".streaminfo.get"
	return &StreamInfoRPC{
		cache: cache.New[StreamInfoResult](streamInfoCacheCapacity, uptimeOfflineTTL),
		request: func(ctx context.Context, req outgressrpc.StreamInfoRequest) (outgressrpc.StreamInfoReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.StreamInfoReply](ctx, nc, subject, req, uptimeRPCTimeout)
		},
	}
}

func (s *StreamInfoRPC) Lookup(ctx context.Context, broadcasterID, login string) (StreamInfoResult, error) {
	return s.cache.GetOrLoadTTL(ctx, streamInfoKey(broadcasterID, login), func(ctx context.Context) (StreamInfoResult, time.Duration, error) {
		reply, err := s.request(ctx, outgressrpc.StreamInfoRequest{BroadcasterID: broadcasterID, TargetLogin: login})
		if err != nil {
			return StreamInfoResult{}, 0, err
		}
		if reply.Error != "" {
			return StreamInfoResult{}, 0, errors.New(reply.Error)
		}
		return streamInfoOf(reply), streamInfoTTL(reply.Live), nil
	})
}

func streamInfoKey(broadcasterID, login string) string {
	if broadcasterID != "" {
		return broadcasterID
	}
	return "login:" + login
}

func streamInfoOf(reply outgressrpc.StreamInfoReply) StreamInfoResult {
	return StreamInfoResult{
		UserFound: reply.UserFound, Live: reply.Live, Title: reply.Title,
		GameName: reply.GameName, ViewerCount: reply.ViewerCount, StartedAt: reply.StartedAt,
	}
}

func streamInfoTTL(live bool) time.Duration {
	if live {
		return uptimePositiveTTL
	}
	return uptimeOfflineTTL
}
