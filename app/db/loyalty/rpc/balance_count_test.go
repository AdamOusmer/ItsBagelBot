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

func TestBalanceCountAtLeastRPCUsesActiveFreeze(t *testing.T) {
	h := newHarness(t)
	req := loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 1}
	frozen := h.call(t, loyaltyrpc.VerbFreezeSet, req)
	require.NotNil(t, frozen.Freeze)
	req.FreezeID = frozen.Freeze.FreezeID
	h.call(t, "balance.add", loyaltyrpc.Request{UserID: "2", TargetViewerID: "7", ViewerLogin: "sender", Value: 5})

	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	mins := engine.BalanceMinimums{Points: 1005}
	withFreeze, frozenErr := client.BalanceCountAtLeast(t.Context(), 2, mins)
	h.call(t, loyaltyrpc.VerbFreezeRelease, req)
	live, liveErr := client.BalanceCountAtLeast(t.Context(), 2, mins)
	require.NoError(t, frozenErr)
	require.NoError(t, liveErr)
	require.Equal(t, []loyaltyrpc.BalanceCount{{FreezeStamp: frozen.Freeze.FreezeStamp}, {Count: 1}}, []loyaltyrpc.BalanceCount{withFreeze, live})
}
