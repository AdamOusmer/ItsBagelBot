// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"testing"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/require"
)

func TestHandleBalanceAdjustCreatesResolvedViewer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    int64
		absolute bool
		points   int64
	}{
		{"set", 5000, true, 5000}, {"add", 5000, false, 5000}, {"remove", -5000, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTransferHarness(t)
			req := loyaltyrpc.Request{UserID: "2", ViewerID: "7", TargetViewerID: "9", ViewerLogin: "@Blemmyz", Value: tc.value}
			reply := l.adjustBalance(t.Context(), req, tc.absolute)
			require.Empty(t, reply.Error)
			require.True(t, reply.Found)
			require.NotNil(t, reply.Balance)
			require.Equal(t, "9", reply.Balance.ViewerID)
			require.Equal(t, "blemmyz", reply.Balance.ViewerLogin)
			require.Equal(t, tc.points, reply.Balance.Points)
		})
	}
}

func TestHandleBalanceAdjustResolvedDeltaAndLegacy(t *testing.T) {
	l := newTransferHarness(t)
	reply := l.handleBalanceAdd(t.Context(), loyaltyrpc.Request{UserID: "2", TargetViewerID: "8", ViewerLogin: "newlogin", Value: 25})
	require.Empty(t, reply.Error)
	require.EqualValues(t, 125, reply.Balance.Points)
	require.Equal(t, "newlogin", reply.Balance.ViewerLogin)
	reply = l.handleBalanceAdd(t.Context(), loyaltyrpc.Request{UserID: "2", TargetViewerID: "8", ViewerLogin: "newlogin", Value: -200})
	require.Empty(t, reply.Error)
	require.Zero(t, reply.Balance.Points)
	// Existing callers keep their login-based API and do not create viewers.
	reply = l.handleBalanceSet(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerLogin: "ghost", Value: 100})
	require.Empty(t, reply.Error)
	require.False(t, reply.Found)
	reply = l.handleBalanceSet(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerLogin: "sender", Value: 100})
	require.Empty(t, reply.Error)
	require.True(t, reply.Found)
	require.Equal(t, "7", reply.Balance.ViewerID)
	require.EqualValues(t, 100, reply.Balance.Points)
}

func TestHandleBalanceAdjustRejectsInvalidResolvedViewer(t *testing.T) {
	l := newTransferHarness(t)
	for _, id := range []string{"0", "-1", "nope", "18446744073709551616"} {
		reply := l.handleBalanceSet(t.Context(), loyaltyrpc.Request{UserID: "2", TargetViewerID: id, ViewerLogin: "blemmyz", Value: 100})
		require.NotEmpty(t, reply.Error)
		require.False(t, reply.Found)
	}
}
