// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/require"
)

func TestBalanceCountAtLeastRPC(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	req := loyaltyrpc.BalanceCountRequest{UserID: "2", MinPoints: 100, MinWatchSeconds: 120}
	require.NoError(t, h.client.Balance.UpdateOneID(1).SetWatchSeconds(120).Exec(t.Context()))
	freeze := h.call(t, loyaltyrpc.VerbFreezeSet, loyaltyrpc.Request{UserID: "2", Holder: "mod", Minutes: 1})
	require.NotNil(t, freeze.Freeze)
	require.NoError(t, h.client.Balance.Update().SetWatchSeconds(120).Exec(t.Context()))
	for _, tc := range []struct {
		name    string
		request loyaltyrpc.BalanceCountRequest
		want    loyaltyrpc.Reply
		code    domainrpc.Code
	}{
		{"snapshot AND thresholds", req, loyaltyrpc.Reply{Total: 1, Found: true, FreezeStamp: freeze.Freeze.FreezeStamp}, domainrpc.CodeOK},
		{"repeat reuses snapshot", req, loyaltyrpc.Reply{Total: 1, Found: true, FreezeStamp: freeze.Freeze.FreezeStamp}, domainrpc.CodeOK},
		{"other channel has no rows", loyaltyrpc.BalanceCountRequest{UserID: "3"}, loyaltyrpc.Reply{Found: true}, domainrpc.CodeOK},
		{"invalid channel", loyaltyrpc.BalanceCountRequest{UserID: "0"}, loyaltyrpc.Reply{}, domainrpc.CodeInvalid},
		{"negative minimum", loyaltyrpc.BalanceCountRequest{UserID: "2", MinPoints: -1}, loyaltyrpc.Reply{}, domainrpc.CodeInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.BalanceCountAtLeast(t.Context(), tc.request)
			if tc.code != domainrpc.CodeOK {
				requireRPCCode(t, err, tc.code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
	released := h.call(t, loyaltyrpc.VerbFreezeRelease, loyaltyrpc.Request{UserID: "2", Holder: "mod", FreezeID: freeze.Freeze.FreezeID})
	require.True(t, released.Found)
	got, err := client.BalanceCountAtLeast(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, loyaltyrpc.Reply{Total: 2, Found: true}, got)
}
