// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

// streamReader is the slice of *twitch.Client the streaminfo handler needs.
//
// It is an interface for one reason: this handler is the only one in the file
// that composes THREE Helix reads (resolve a login, read the stream, fall back
// to the channel when offline), and the order it composes them in is the part
// worth pinning in a test. *twitch.Client keeps its token plumbing unexported,
// so a test in this package cannot build one with a stubbed transport; naming
// the three calls here lets the composition be tested without loosening the
// client's own encapsulation.
type streamReader interface {
	UserIDByLogin(ctx context.Context, login string) (string, error)
	StreamDetails(ctx context.Context, broadcasterID string) (twitch.StreamDetails, bool, error)
	ChannelInfo(ctx context.Context, broadcasterID string) (twitch.ChannelInfo, error)
}

// readStreamInfo answers one streaminfo.get: the channel's live flag, title,
// category, viewer count and session start, for a broadcaster id or a login.
//
// The offline path is the reason this is more than a StreamDetails passthrough.
// Get Streams answers nothing at all for an offline channel, but a custom
// command's {title} / {game} have to read the same values !title and !game
// print, and those work offline — so an offline channel pays one extra Get
// Channel Information (the very call !title makes) for its title and category.
// A live channel pays nothing extra: Get Streams already carries both, fresher
// than the channel object, so the fallback fires only where it is the only
// source.
func readStreamInfo(ctx context.Context, r streamReader, log *zap.Logger, req outgressrpc.StreamInfoRequest) outgressrpc.StreamInfoReply {
	if req.BroadcasterID == "" && req.TargetLogin == "" {
		return outgressrpc.StreamInfoReply{Error: "bad request"}
	}
	id, err := resolveStreamTarget(ctx, r, req)
	if err != nil {
		log.Warn("streaminfo target resolve failed", zap.Error(err))
		return outgressrpc.StreamInfoReply{Error: "lookup failed"}
	}
	if id == "" {
		return outgressrpc.StreamInfoReply{UserFound: false}
	}
	return readStreamOf(ctx, r, log, id)
}

// resolveStreamTarget turns the request's addressing into a broadcaster id. An
// empty id with no error is a login Twitch does not know, which is a resolved
// answer rather than a failure.
func resolveStreamTarget(ctx context.Context, r streamReader, req outgressrpc.StreamInfoRequest) (string, error) {
	if req.BroadcasterID != "" {
		return req.BroadcasterID, nil
	}
	return r.UserIDByLogin(ctx, req.TargetLogin)
}

// readStreamOf reads one resolved channel: the live snapshot first, the
// offline channel object only when it says the channel is dark.
func readStreamOf(ctx context.Context, r streamReader, log *zap.Logger, id string) outgressrpc.StreamInfoReply {
	details, live, err := r.StreamDetails(ctx, id)
	if err != nil {
		log.Warn("streaminfo lookup failed", zap.Error(err))
		return outgressrpc.StreamInfoReply{UserFound: true, Error: "lookup failed"}
	}
	if !live {
		return offlineStreamInfo(ctx, r, log, id)
	}
	return outgressrpc.StreamInfoReply{
		UserFound: true, Live: true, Title: details.Title, GameName: details.GameName,
		ViewerCount: details.ViewerCount, StartedAt: details.StartedAt,
	}
}

// offlineStreamInfo fills an offline channel's title and category from Get
// Channel Information.
//
// A failure there is logged and dropped rather than turned into Error, and
// that asymmetry is deliberate: the stream read already succeeded, so "this
// channel is offline" is a fact the caller can use (a viewer count of zero, an
// empty uptime). Reporting the whole reply as failed to say the title was
// unreadable would throw away the answer we do have and make a known-offline
// channel indistinguishable from an unreachable Twitch.
func offlineStreamInfo(ctx context.Context, r streamReader, log *zap.Logger, id string) outgressrpc.StreamInfoReply {
	info, err := r.ChannelInfo(ctx, id)
	if err != nil {
		log.Warn("streaminfo offline channel read failed", zap.Error(err))
		return outgressrpc.StreamInfoReply{UserFound: true}
	}
	return outgressrpc.StreamInfoReply{UserFound: true, Title: info.Title, GameName: info.GameName}
}
