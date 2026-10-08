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

func TestChannelCountsReplies(t *testing.T) {
	tests := []struct {
		name  string
		reads *fakeCounts
		req   outgressrpc.ChannelCountsRequest
		want  outgressrpc.ChannelCountsReply
	}{
		{"refuses a request addressing nobody", &fakeCounts{}, outgressrpc.ChannelCountsRequest{}, outgressrpc.ChannelCountsReply{Error: "bad request"}},
		{
			"reads both halves", &fakeCounts{followers: 100, followersOK: true, subs: 7, subsOK: true},
			outgressrpc.ChannelCountsRequest{BroadcasterID: "123"},
			outgressrpc.ChannelCountsReply{Followers: 100, FollowersOK: true, Subs: 7, SubsOK: true},
		},
		{
			"keeps followers when subs are unavailable for a missing scope", &fakeCounts{followers: 100, followersOK: true},
			outgressrpc.ChannelCountsRequest{BroadcasterID: "123"}, outgressrpc.ChannelCountsReply{Followers: 100, FollowersOK: true},
		},
		{
			"keeps subs when the followers read errors", &fakeCounts{subs: 7, subsOK: true, followersErr: errors.New("boom")},
			outgressrpc.ChannelCountsRequest{BroadcasterID: "123"}, outgressrpc.ChannelCountsReply{Subs: 7, SubsOK: true},
		},
		{
			"degrades subs quietly without a broadcaster token", &fakeCounts{followers: 100, followersOK: true, subsErr: twitch.ErrNoUserToken},
			outgressrpc.ChannelCountsRequest{BroadcasterID: "123"}, outgressrpc.ChannelCountsReply{Followers: 100, FollowersOK: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, readCounts(tt.reads, tt.req))
		})
	}
}

func TestChannelCountsNamingNoChannelCostsNoHelixCall(t *testing.T) {
	f := &fakeCounts{}

	readCounts(f, outgressrpc.ChannelCountsRequest{})

	assert.Empty(t, f.calls)
}
