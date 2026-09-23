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

// channelCountsReader is the slice of *twitch.Client the channelcounts
// handler needs. An interface for the same reason streamReader is one: it
// composes two independent reads under two different identities, and naming
// them here lets that composition be tested without loosening the client's
// own encapsulation.
type channelCountsReader interface {
	FollowersTotal(ctx context.Context, broadcasterID string) (int, bool, error)
	SubscriptionsTotal(ctx context.Context, broadcasterID string) (int, bool, error)
}

// readChannelCounts resolves {followers} and {subs} in one call, each under
// the identity Twitch requires for it (see FollowersTotal/SubscriptionsTotal).
// The two reads are independent on purpose: a broadcaster's grant can carry
// one scope and not the other, and a 401/403 on either one already degrades
// to OK=false rather than an error (helixTotal), so a failed READ on one half
// only logs and leaves that half's OK false — it never blanks the other half
// or sets Error, which is reserved for a request that could not be attempted
// at all.
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

// readCountsConcurrently runs both Helix reads in parallel. They ride two
// different identities (bot, broadcaster), so nothing on the Twitch side
// serializes them, and sesame's own RPC timeout (channelCountsRPCTimeout,
// 3.5s) is tighter than this handler's budget (followageHandleTimeout, 4s):
// run serially, two Helix round trips can together exceed sesame's deadline
// before this handler even finishes composing its reply. Each goroutine
// writes only its own pair of named returns, so wg.Wait() is the only
// synchronization the two need.
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

// readSubs reads under the broadcaster's own token, which most channels have
// never granted at all (only the bot's own grant is required to enroll) —
// ErrNoUserToken is that steady state, not a fault, so it logs at Debug
// rather than the Warn every other lookup failure gets.
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
