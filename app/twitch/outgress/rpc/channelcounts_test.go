// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type fakeCounts struct {
	followers    int
	followersOK  bool
	followersErr error
	subs         int
	subsOK       bool
	subsErr      error

	mu    sync.Mutex
	calls []string
}

func (f *fakeCounts) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeCounts) FollowersTotal(_ context.Context, broadcasterID string) (int, bool, error) {
	f.record("followers:" + broadcasterID)
	return f.followers, f.followersOK, f.followersErr
}

func (f *fakeCounts) SubscriptionsTotal(_ context.Context, broadcasterID string) (int, bool, error) {
	f.record("subs:" + broadcasterID)
	return f.subs, f.subsOK, f.subsErr
}

func readCounts(r channelCountsReader, req outgressrpc.ChannelCountsRequest) outgressrpc.ChannelCountsReply {
	return readChannelCounts(context.Background(), r, zap.NewNop(), req)
}

func TestChannelCountsRefusesARequestAddressingNobody(t *testing.T) {
	f := &fakeCounts{}
	assert.Equal(t, "bad request", readCounts(f, outgressrpc.ChannelCountsRequest{}).Error)
	assert.Empty(t, f.calls, "a request naming no channel costs no Helix call")
}

func TestChannelCountsReadsBothHalves(t *testing.T) {
	f := &fakeCounts{followers: 100, followersOK: true, subs: 7, subsOK: true}
	got := readCounts(f, outgressrpc.ChannelCountsRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.ChannelCountsReply{
		Followers: 100, FollowersOK: true, Subs: 7, SubsOK: true,
	}, got)
	assert.ElementsMatch(t, []string{"followers:123", "subs:123"}, f.calls)
}

func TestChannelCountsOneHalfMissingScopeDoesNotBlankTheOther(t *testing.T) {
	f := &fakeCounts{followers: 100, followersOK: true, subsOK: false}
	got := readCounts(f, outgressrpc.ChannelCountsRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.ChannelCountsReply{Followers: 100, FollowersOK: true}, got)
	assert.Empty(t, got.Error, "a degraded half is not a request-level failure")
}

func TestChannelCountsOneHalfReadErrorDoesNotFailTheReply(t *testing.T) {
	f := &fakeCounts{subs: 7, subsOK: true, followersErr: errors.New("boom")}
	got := readCounts(f, outgressrpc.ChannelCountsRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.ChannelCountsReply{Subs: 7, SubsOK: true}, got)
	assert.False(t, got.FollowersOK)
	assert.Empty(t, got.Error)
}

func TestChannelCountsSubsWithNoBroadcasterTokenDegradesQuietly(t *testing.T) {
	f := &fakeCounts{followers: 100, followersOK: true, subsErr: twitch.ErrNoUserToken}
	got := readCounts(f, outgressrpc.ChannelCountsRequest{BroadcasterID: "123"})

	assert.Equal(t, outgressrpc.ChannelCountsReply{Followers: 100, FollowersOK: true}, got)
	assert.Empty(t, got.Error)
}
