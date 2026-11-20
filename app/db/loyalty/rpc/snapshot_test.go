// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/engine/eligibility"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createRPCSnapshot(t *testing.T, client *engine.LoyaltyRPC) engine.SnapshotRef {
	t.Helper()
	req := engine.SnapshotRequest{BroadcasterID: 2, Owner: "round:first", ExpiresAt: time.Now().Add(time.Hour)}
	ref, err := client.SnapshotCreate(t.Context(), req)
	require.NoError(t, err)
	require.NotEmpty(t, ref.SnapshotID)
	return ref
}

func TestSnapshotRPCReadRoutes(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	_, err := client.FreezeSet(t.Context(), 2, 1, 7)
	require.NoError(t, err)
	public := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2"})
	private := createRPCSnapshot(t, client)
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 5})
	for _, tc := range []struct {
		name, id        string
		channel, viewer uint64
		want            loyaltyrpc.BalanceSnapshot
		code            domainrpc.Code
	}{
		{name: "live during pause", channel: 2, viewer: 7, want: loyaltyrpc.BalanceSnapshot{Found: true, Points: 1005}},
		{name: "private copy", channel: 2, viewer: 7, id: private.SnapshotID, want: loyaltyrpc.BalanceSnapshot{Found: true, Points: 1000}},
		{name: "absent viewer", channel: 2, viewer: 9, id: private.SnapshotID},
		{name: "wrong channel", channel: 3, viewer: 7, id: private.SnapshotID, code: loyaltyrpc.RefusalSnapshotGone},
		{name: "public pin refused", channel: 2, viewer: 7, id: public.FreezeID, code: loyaltyrpc.RefusalSnapshotGone},
		{name: "missing", channel: 2, viewer: 7, id: "missing", code: loyaltyrpc.RefusalSnapshotGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := engine.BalanceSnapshotRequest{BroadcasterID: tc.channel, ViewerID: tc.viewer, SnapshotID: tc.id}
			got, err := client.BalanceSnapshot(t.Context(), req)
			if tc.code != "" {
				requireRPCCode(t, err, tc.code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	pinned := h.call(t, "top.get", loyaltyrpc.Request{UserID: "2", FreezeID: private.SnapshotID})
	assert.Equal(t, domainrpc.Refused(domainrpc.CodeConflict, loyaltyrpc.RefusalStalePin), pinned.Refusal)
}

func TestSnapshotRPCIdempotenceAndDrop(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	first := createRPCSnapshot(t, client)
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 5})
	req := engine.SnapshotRequest{BroadcasterID: 2, Owner: "round:first", ExpiresAt: time.Now().Add(2 * time.Hour)}
	retry, err := client.SnapshotCreate(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first, retry)
	for _, expected := range []bool{true, false} {
		var reply loyaltyrpc.SnapshotDropped
		req := loyaltyrpc.SnapshotDropRequest{UserID: "2", SnapshotID: first.SnapshotID}
		msg, err := codec.Marshal(req)
		require.NoError(t, err)
		res, err := h.nc.Request("loyalty."+loyaltyrpc.VerbSnapshotDrop, msg, time.Second)
		require.NoError(t, err)
		require.NoError(t, codec.Unmarshal(res.Data, &reply))
		assert.Equal(t, loyaltyrpc.SnapshotDropped{Dropped: expected}, reply)
	}
	_, err = client.BalanceCountAtLeast(t.Context(), 2, engine.BalanceMinimums{SnapshotID: first.SnapshotID})
	requireRPCCode(t, err, loyaltyrpc.RefusalSnapshotGone)
	_, err = client.SnapshotCreate(t.Context(), req)
	requireRPCCode(t, err, loyaltyrpc.RefusalSnapshotGone)
}

type snapshotLoyalty struct {
	engine.LoyaltyStore
	rpc *engine.LoyaltyRPC
}

func (s snapshotLoyalty) BalanceSnapshot(ctx context.Context, req engine.BalanceSnapshotRequest) (loyaltyrpc.BalanceSnapshot, error) {
	return s.rpc.BalanceSnapshot(ctx, req)
}

func TestGiveawayJoinEligibilityAfterPointsMove(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	cutoff := createRPCSnapshot(t, client)
	moved := h.call(t, "balance.transfer", loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "8", ViewerLogin: "receiver", Value: 500})
	require.True(t, moved.Spent)
	checker := engine.NewEligibility(snapshotLoyalty{rpc: client}, nil, nil, nil)
	t.Cleanup(checker.Close)
	for _, tc := range []struct {
		name   string
		viewer uint64
		id     string
		pass   bool
	}{
		{"sender still qualifies at open", 7, cutoff.SnapshotID, true},
		{"recipient still falls short at open", 8, cutoff.SnapshotID, false},
		{"sender would fall short live", 7, "", false},
		{"recipient would qualify live", 8, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			who := eligibility.Joiner{Channel: 2, Viewer: tc.viewer, Kind: draw.KindGiveaway,
				Cutoff: eligibility.Cutoff{ID: tc.id, OpenedAt: time.UnixMilli(cutoff.AsOf)}}
			verdict, err := checker.Join(t.Context(), eligibility.Rules{MinPoints: 600}, who)
			require.NoError(t, err)
			require.Equal(t, tc.pass, verdict.Pass)
		})
	}
}

func TestStandingsRPCWireFields(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		verb string
		body string
		keys []string
	}{
		{loyaltyrpc.VerbFreezeSet, `{"user_id":"2","minutes":1,"actor_id":"7"}`, []string{"frozen_as_of", "expires_at"}},
		{loyaltyrpc.VerbFreezeGet, `{"user_id":"2"}`, []string{"active", "frozen_as_of", "expires_at"}},
		{loyaltyrpc.VerbFreezeRelease, `{"user_id":"2","actor_id":"8"}`, []string{"released"}},
		{loyaltyrpc.VerbFreezeGet, `{"user_id":"2"}`, []string{"active", "frozen_as_of", "expires_at"}},
		{loyaltyrpc.VerbSnapshotCreate, `{"user_id":"2","owner":"round:wire","expires_at":4102444800000}`, []string{"snapshot_id", "as_of"}},
		{loyaltyrpc.VerbSnapshotDrop, `{"user_id":"2","snapshot_id":"missing"}`, []string{"dropped"}},
		{loyaltyrpc.VerbBalanceSnapshot, `{"user_id":"2","viewer_id":"7"}`, []string{"points", "watch_seconds", "found"}},
		{loyaltyrpc.VerbBalanceCountAtLeast, `{"user_id":"2"}`, []string{"count"}},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			msg, err := h.nc.Request("loyalty."+tc.verb, []byte(tc.body), time.Second)
			require.NoError(t, err)
			var fields map[string]codec.RawMessage
			require.NoError(t, codec.Unmarshal(msg.Data, &fields))
			require.Len(t, fields, len(tc.keys))
			for _, key := range tc.keys {
				assert.Contains(t, fields, key)
			}
		})
	}
}
