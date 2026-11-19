// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFreezeRPCValidatesHolderDurationAndCAS(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name string
		verb string
		req  loyaltyrpc.Request
	}{
		{"invalid channel", loyaltyrpc.VerbFreezeGet, loyaltyrpc.Request{UserID: "0"}},
		{"unknown holder", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "viewer", Minutes: 1}},
		{"empty round id", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "round:", Minutes: 1}},
		{"punctuated round id", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "round:r_1", Minutes: 1}},
		{"long round id", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "round:" + strings.Repeat("a", 65), Minutes: 1}},
		{"zero minutes", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 0}},
		{"over cap", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 31}},
		{"unconditional round release", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "round:abc"}},
		{"invalid release holder", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "ROUND:r", FreezeID: "id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, domainrpc.CodeInvalid, h.call(t, tc.verb, tc.req).Code)
		})
	}
}

func TestFreezeRPCPublishesMarkerOnlyOnChange(t *testing.T) {
	h := newHarness(t)
	sub, err := h.nc.SubscribeSync(invalidationPrefix + ".loyalty")
	require.NoError(t, err)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	cmd := engine.FreezeCommand{BroadcasterID: 2, Holder: "mod", Minutes: 2, BlockTransfer: true, CloseGames: true}
	view, changed, err := client.FreezeSet(t.Context(), cmd)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, []any{[]string{"mod"}, true, true, int64(120000)},
		[]any{view.Holders, view.BlockTransfer, view.CloseGames, view.ExpiresAt - view.FrozenAsOf})

	for _, tc := range []struct {
		name   string
		verb   string
		req    loyaltyrpc.Request
		found  bool
		reason string
	}{
		{"retry", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 2}, false, ""},
		{"join round", loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "round:r-1", Minutes: 2}, true, ""},
		{"stale release", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "mod", FreezeID: "old"}, false, ""},
		{"held release", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "mod", FreezeID: view.FreezeID}, false, loyaltyrpc.RefusalHeldByRound},
		{"round releases", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "round:r-1", FreezeID: view.FreezeID}, true, ""},
		{"mod releases", loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "mod", FreezeID: view.FreezeID}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.call(t, tc.verb, tc.req)
			assert.Equal(t, tc.found, reply.Found)
			assert.Equal(t, tc.reason, reply.Error)
		})
	}
	for range 4 {
		msg, err := sub.NextMsg(time.Second)
		require.NoError(t, err)
		var event invalidate.DTO
		require.NoError(t, codec.Unmarshal(msg.Data, &event))
		assert.Equal(t, invalidate.DTO{BroadcasterID: "2", Keys: []string{invalidate.LoyaltyFreezeMarker}}, event)
	}
	_, err = sub.NextMsg(50 * time.Millisecond)
	assert.ErrorIs(t, err, nats.ErrTimeout)
	got := h.call(t, loyaltyrpc.VerbFreezeGet, loyaltyrpc.Request{UserID: "2"})
	assert.Equal(t, loyaltyrpc.Reply{Freeze: &loyaltyrpc.FreezeView{Holders: []string{}}}, got)
}

func TestFreezeRPCRoutesLeaderboardPins(t *testing.T) {
	h := newHarness(t)
	frozen := h.call(t, loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 1})
	require.NotNil(t, frozen.Freeze)
	require.NotEmpty(t, frozen.Freeze.FreezeID)
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 1})
	for _, tc := range []struct {
		name    string
		id      string
		live    bool
		points  int64
		refusal domainrpc.Refusal
	}{
		{name: "un-pinned active", points: 1000},
		{name: "live pin", live: true, points: 1001},
		{name: "explicit pin", id: frozen.Freeze.FreezeID, points: 1000},
		{name: "stale pin", id: "missing", refusal: domainrpc.Refused(domainrpc.CodeConflict, loyaltyrpc.RefusalStalePin)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", FreezeID: tc.id, Live: tc.live, Limit: 1})
			assert.Equal(t, tc.refusal, reply.Refusal)
			if tc.refusal.Code != "" {
				return
			}
			want := frozen.Freeze.FreezeStamp
			if tc.live {
				want = loyaltyrpc.FreezeStamp{}
			}
			require.Len(t, reply.Top, 1)
			assert.Equal(t, []any{want, tc.points}, []any{reply.FreezeStamp, reply.Top[0].Points})
		})
	}
}

func TestFreezeRPCPinsOldEpochAndSnapshots(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.client.Balance.Update().SetWatchSeconds(120).Exec(t.Context()))
	req := loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 1, BlockTransfer: true, CloseGames: true}
	frozen := h.call(t, loyaltyrpc.VerbFreezeSet, req)
	require.NotNil(t, frozen.Freeze)
	stamp := frozen.Freeze.FreezeStamp
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 5})
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, tc := range []struct {
		viewer uint64
		want   loyaltyrpc.BalanceSnapshot
	}{
		{7, loyaltyrpc.BalanceSnapshot{Found: true, Points: 1000, WatchSeconds: 120, FreezeStamp: stamp}},
		{9, loyaltyrpc.BalanceSnapshot{FreezeStamp: stamp}},
	} {
		got, err := client.BalanceSnapshot(t.Context(), 2, tc.viewer)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}

	req.FreezeID = stamp.FreezeID
	release := h.call(t, loyaltyrpc.VerbFreezeRelease, req)
	require.True(t, release.Found)
	pinned := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", FreezeID: stamp.FreezeID, Limit: 1})
	live := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", Limit: 1})
	require.Len(t, pinned.Top, 1)
	require.Len(t, live.Top, 1)
	assert.Equal(t, []any{stamp.FreezeID, stamp.FrozenAsOf, int64(1000), "", int64(1005)},
		[]any{pinned.FreezeID, pinned.FrozenAsOf, pinned.Top[0].Points, live.FreezeID, live.Top[0].Points})
}

func TestFreezeRPCMutationRefusals(t *testing.T) {
	h := newHarness(t)
	req := loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 1, BlockTransfer: true, CloseGames: true}
	require.True(t, h.call(t, loyaltyrpc.VerbFreezeSet, req).Found)
	for _, tc := range []struct {
		verb   string
		reason string
	}{
		{"balance.reset", loyaltyrpc.RefusalFrozen},
		{"balance.transfer", loyaltyrpc.RefusalFrozenTransfers},
		{"balance.wager", loyaltyrpc.RefusalFrozenGames},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			reply := h.call(t, tc.verb, loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "8", ViewerLogin: "receiver", Value: 1})
			assert.Equal(t, domainrpc.Refused(domainrpc.CodeConflict, tc.reason), reply.Refusal)
		})
	}
}
