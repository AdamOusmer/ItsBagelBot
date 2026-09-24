// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

type streamReader interface {
	UserIDByLogin(ctx context.Context, login string) (string, error)
	StreamDetails(ctx context.Context, broadcasterID string) (twitch.StreamDetails, bool, error)
	ChannelInfo(ctx context.Context, broadcasterID string) (twitch.ChannelInfo, error)
}

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

func resolveStreamTarget(ctx context.Context, r streamReader, req outgressrpc.StreamInfoRequest) (string, error) {
	if req.BroadcasterID != "" {
		return req.BroadcasterID, nil
	}
	return r.UserIDByLogin(ctx, req.TargetLogin)
}

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

func offlineStreamInfo(ctx context.Context, r streamReader, log *zap.Logger, id string) outgressrpc.StreamInfoReply {
	info, err := r.ChannelInfo(ctx, id)
	if err != nil {
		log.Warn("streaminfo offline channel read failed", zap.Error(err))
		return outgressrpc.StreamInfoReply{UserFound: true}
	}
	return outgressrpc.StreamInfoReply{UserFound: true, Title: info.Title, GameName: info.GameName}
}
