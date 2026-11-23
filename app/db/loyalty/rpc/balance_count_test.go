// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	"ItsBagelBot/app/twitch/sesame/engine"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/require"
)

func TestBalanceCountAtLeastRPC(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.client.Balance.Update().Where(balance.ViewerIDEQ(7)).SetWatchSeconds(600).Exec(t.Context()))
	require.NoError(t, h.client.Balance.Create().SetUserID(3).SetViewerID(7).SetPoints(1000).SetWatchSeconds(600).Exec(t.Context()))
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, tc := range []struct {
		name string
		mins engine.BalanceMinimums
		want int64
	}{
		{"all standings", engine.BalanceMinimums{}, 2},
		{"inclusive points", engine.BalanceMinimums{Points: 100}, 2},
		{"inclusive watch", engine.BalanceMinimums{WatchSeconds: 600}, 1},
		{"both inclusive", engine.BalanceMinimums{Points: 1000, WatchSeconds: 600}, 1},
		{"watch too high", engine.BalanceMinimums{Points: 100, WatchSeconds: 601}, 0},
		{"points too high", engine.BalanceMinimums{Points: 1001}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.BalanceCountAtLeast(t.Context(), 2, tc.mins)
			require.NoError(t, err)
			require.Equal(t, loyaltyrpc.BalanceCount{Count: tc.want}, got)
		})
	}
	_, err := client.BalanceCountAtLeast(t.Context(), 2, engine.BalanceMinimums{Points: -1})
	requireRPCCode(t, err, domainrpc.CodeInvalid)
}

func TestBalanceCountAtLeastRPCUsesOnlyRequestedSnapshot(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	_, err := client.FreezeSet(t.Context(), 2, 1)
	require.NoError(t, err)
	snapshot := createRPCSnapshot(t, client)
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 5})
	for _, tc := range []struct {
		id    string
		count int64
		code  domainrpc.Code
	}{
		{id: "", count: 1},
		{id: snapshot.SnapshotID, count: 0},
		{id: "missing", code: loyaltyrpc.RefusalSnapshotGone},
	} {
		t.Run(tc.id, func(t *testing.T) {
			mins := engine.BalanceMinimums{Points: 1005, SnapshotID: tc.id}
			got, err := client.BalanceCountAtLeast(t.Context(), 2, mins)
			if tc.code != "" {
				requireRPCCode(t, err, tc.code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, loyaltyrpc.BalanceCount{Count: tc.count}, got)
		})
	}
}
