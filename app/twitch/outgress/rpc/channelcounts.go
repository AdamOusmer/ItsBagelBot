// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"sync"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

type channelCountsReader interface {
	FollowersTotal(ctx context.Context, broadcasterID string) (int, bool, error)
	SubscriptionsTotal(ctx context.Context, broadcasterID string) (int, bool, error)
}

func readChannelCounts(ctx context.Context, r channelCountsReader, log *zap.Logger, req outgressrpc.ChannelCountsRequest) outgressrpc.ChannelCountsReply {
	if req.BroadcasterID == "" {
		return outgressrpc.ChannelCountsReply{Error: "bad request"}
	}
	followers, followersOK, subs, subsOK := readCountsConcurrently(ctx, r, log, req.BroadcasterID)
	return outgressrpc.ChannelCountsReply{
		Followers: followers, FollowersOK: followersOK,
		Subs: subs, SubsOK: subsOK,
	}
}

func readCountsConcurrently(ctx context.Context, r channelCountsReader, log *zap.Logger, broadcasterID string) (followers int, followersOK bool, subs int, subsOK bool) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		followers, followersOK = readFollowers(ctx, r, log, broadcasterID)
	}()
	go func() {
		defer wg.Done()
		subs, subsOK = readSubs(ctx, r, log, broadcasterID)
	}()
	wg.Wait()
	return followers, followersOK, subs, subsOK
}

func readFollowers(ctx context.Context, r channelCountsReader, log *zap.Logger, broadcasterID string) (int, bool) {
	followers, ok, err := r.FollowersTotal(ctx, broadcasterID)
	if err != nil {
		log.Warn("channelcounts: followers lookup failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
	return followers, ok
}

func readSubs(ctx context.Context, r channelCountsReader, log *zap.Logger, broadcasterID string) (int, bool) {
	subs, ok, err := r.SubscriptionsTotal(ctx, broadcasterID)
	if err != nil {
		if errors.Is(err, twitch.ErrNoUserToken) {
			log.Debug("channelcounts: subs lookup skipped, no broadcaster token", zap.String("broadcaster_id", broadcasterID))
		} else {
			log.Warn("channelcounts: subs lookup failed", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		}
	}
	return subs, ok
}
