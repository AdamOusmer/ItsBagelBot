// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeRPCValidatesIdentityAndDuration(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, tc := range []struct {
		name    string
		channel draw.ChannelID
		actor   draw.ViewerID
		minutes engine.FreezeMinutes
	}{
		{"invalid channel", 0, "7", 1},
		{"invalid actor", 2, "0", 1},
		{"zero minutes", 2, "7", 0},
		{"over cap", 2, "7", 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.FreezeSet(t.Context(), tc.channel, tc.minutes, tc.actor)
			requireRPCCode(t, err, domainrpc.CodeInvalid)
		})
	}
}

func TestFreezeRPCPublishesMarkerOnlyOnChange(t *testing.T) {
	h := newHarness(t)
	sub, err := h.nc.SubscribeSync(invalidationPrefix + ".loyalty")
	require.NoError(t, err)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, minutes := range []engine.FreezeMinutes{2, 4} {
		view, err := client.FreezeSet(t.Context(), 2, minutes, "7")
		require.NoError(t, err)
		assert.Equal(t, int64(minutes)*time.Minute.Milliseconds(), view.ExpiresAt-view.FrozenAsOf)
		checkFreezeStatus(t, client, loyaltyrpc.FreezeView{Active: true, FreezeTimes: view})
	}
	for _, want := range []bool{true, false} {
		released, err := client.FreezeRelease(t.Context(), 2, "8")
		require.NoError(t, err)
		assert.Equal(t, want, released)
	}
	checkFreezeStatus(t, client, loyaltyrpc.FreezeView{})
	checkFreezeMarkers(t, sub)
}

func checkFreezeStatus(t *testing.T, client *engine.LoyaltyRPC, want loyaltyrpc.FreezeView) {
	t.Helper()
	got, err := client.FreezeGet(t.Context(), 2)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func checkFreezeMarkers(t *testing.T, sub *nats.Subscription) {
	t.Helper()
	for range 3 {
		msg, err := sub.NextMsg(time.Second)
		require.NoError(t, err)
		var event invalidate.DTO
		require.NoError(t, codec.Unmarshal(msg.Data, &event))
		assert.Equal(t, invalidate.DTO{BroadcasterID: "2", Keys: []string{invalidate.LoyaltyFreezeMarker}}, event)
	}
	_, err := sub.NextMsg(50 * time.Millisecond)
	assert.ErrorIs(t, err, nats.ErrTimeout)
}

func TestFreezeRPCRoutesLeaderboardPins(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	_, err := client.FreezeSet(t.Context(), 2, 1, "7")
	require.NoError(t, err)
	stamp := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2"}).FreezeStamp
	require.NotEmpty(t, stamp.FreezeID)
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 1})
	for _, tc := range []struct {
		name          string
		id            string
		live, release bool
		points        int64
		refusal       domainrpc.Refusal
	}{
		{name: "active", points: 1000},
		{name: "live pin", live: true, points: 1001},
		{name: "explicit pin", id: stamp.FreezeID, points: 1000},
		{name: "stale pin", id: "missing", refusal: domainrpc.Refused(domainrpc.CodeConflict, loyaltyrpc.RefusalStalePin)},
		{name: "pin after release", id: stamp.FreezeID, release: true, points: 1000},
		{name: "live after release", points: 1001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.release {
				released, err := client.FreezeRelease(t.Context(), 2, "8")
				require.NoError(t, err)
				require.True(t, released)
			}
			reply := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", FreezeID: tc.id, Live: tc.live, Limit: 1})
			require.Equal(t, tc.refusal, reply.Refusal)
			if tc.refusal.Code != "" {
				return
			}
			require.Len(t, reply.Top, 1)
			assert.Equal(t, tc.points, reply.Top[0].Points)
			assert.Equal(t, tc.points == 1000, reply.FreezeID == stamp.FreezeID)
		})
	}
}

func TestFreezeRPCOnlyBlocksReset(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	_, err := client.FreezeSet(t.Context(), 2, 1, "7")
	require.NoError(t, err)
	req := loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "8", ViewerLogin: "receiver", Value: 1}
	for _, tc := range []struct {
		verb string
		want domainrpc.Refusal
	}{
		{"balance.reset", domainrpc.Refused(domainrpc.CodeConflict, loyaltyrpc.RefusalFrozen)},
		{"balance.transfer", domainrpc.Refusal{}},
		{"balance.wager", domainrpc.Refusal{}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			reply := h.call(t, tc.verb, req)
			assert.Equal(t, tc.want, reply.Refusal)
			assert.Equal(t, tc.want.Code == "", reply.Spent)
		})
	}
}
