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

// streamInfoCacheCapacity ceilings the stream-info cache. It is keyed per
// channel — the broadcaster running the command, plus any login their
// templates name — so it grows with distinct CHANNELS, not with viewers. That
// is a far smaller set than followage's per-viewer keyspace and a slightly
// larger one than uptime's own per-broadcaster cache, since a shoutout
// template can name a channel this bot is not even in.
const streamInfoCacheCapacity int64 = 4096

// StreamInfoResult is one channel's current session as the response tokens
// read it.
//
// UserFound separates "Twitch has no such channel" from "that channel is
// offline": the first renders every token in the family empty, while the
// second is a real answer with a real title and a viewer count of zero. A
// zero-value convention could not tell them apart, and a template naming a
// misspelled login would then claim the channel exists and is dark.
type StreamInfoResult struct {
	UserFound   bool
	Live        bool
	Title       string
	GameName    string
	ViewerCount int
	StartedAt   time.Time
}

// StreamInfoLookup reads one channel's live session, addressed by broadcaster
// id or by login. Exactly one is supplied: the id for the channel the command
// ran in, the login for a template naming somebody else's.
type StreamInfoLookup interface {
	Lookup(ctx context.Context, broadcasterID, login string) (StreamInfoResult, error)
}

// StreamInfoRPC is Sesame's cached stream-info reader. Outgress supplies only
// the authenticated Twitch read (including the Get Users hop a login needs —
// sesame never calls Helix); command freshness, singleflight and cache policy
// live here with the command runtime, exactly as they do for !uptime.
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

// Lookup reads one channel, through the cache.
//
// The TTLs are uptime's own (uptimePositiveTTL / uptimeOfflineTTL), reused
// rather than re-picked: {uptime} and !uptime describe the same clock to the
// same chat, and two caches that aged differently would let a command say the
// stream has been up for an hour while the token beside it still said it was
// offline. A live channel is the shorter-lived entry because the viewer count
// on it moves; an offline one changes only when the stream starts.
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

// streamInfoKey namespaces the login-addressed entries away from the
// id-addressed ones, so a channel whose numeric id happens to spell a login
// cannot collide with it.
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
