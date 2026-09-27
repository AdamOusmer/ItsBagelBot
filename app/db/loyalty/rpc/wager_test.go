// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"math"
	"testing"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/require"
)

func TestHandleBalanceWagerOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		won    bool
		points int64
	}{
		{"win", true, 1400}, {"loss", false, 600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newTransferHarness(t)
			reply := l.handleBalanceWager(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerID: "7", Value: 400, Won: tc.won})
			require.Empty(t, reply.Error)
			require.True(t, reply.Found)
			require.True(t, reply.Spent)
			require.False(t, reply.LimitExceeded)
			require.NotNil(t, reply.Balance)
			require.Equal(t, tc.points, reply.Balance.Points)
		})
	}
}

func TestHandleBalanceWagerRefusesWithoutDebit(t *testing.T) {
	l := newTransferHarness(t)
	reply := l.handleBalanceWager(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerID: "7", Value: 5000, Won: true})
	require.Empty(t, reply.Error)
	require.True(t, reply.Found)
	require.False(t, reply.Spent)
	require.False(t, reply.LimitExceeded)
	require.EqualValues(t, 1000, reply.Balance.Points)
	_, _, err := l.repo.BalanceAdjustViewer(t.Context(), repository.BalanceAdjustment{UserID: 2, ViewerID: 7, ViewerLogin: "sender", Value: math.MaxInt64, Absolute: true})
	require.NoError(t, err)
	reply = l.handleBalanceWager(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerID: "7", Value: 1, Won: true})
	require.Empty(t, reply.Error)
	require.False(t, reply.Spent)
	require.True(t, reply.LimitExceeded)
	require.EqualValues(t, math.MaxInt64, reply.Balance.Points)
	require.Equal(t, "9223372036854775807", reply.Balance.PointsExact)
	reply = l.handleBalanceWager(t.Context(), loyaltyrpc.Request{UserID: "2", ViewerID: "99", Value: 1, Won: true})
	require.Empty(t, reply.Error)
	require.False(t, reply.Found)
	require.False(t, reply.Spent)
	require.Nil(t, reply.Balance)
}

func TestHandleBalanceWagerInvalid(t *testing.T) {
	l := newTransferHarness(t)
	for _, req := range []loyaltyrpc.Request{
		{UserID: "0", ViewerID: "7", Value: 1},
		{UserID: "2", ViewerID: "0", Value: 1},
		{UserID: "2", ViewerID: "invalid", Value: 1},
		{UserID: "2", ViewerID: "7", Value: 0},
		{UserID: "2", ViewerID: "7", Value: -1},
	} {
		reply := l.handleBalanceWager(t.Context(), req)
		require.NotEmpty(t, reply.Error)
		require.False(t, reply.Spent)
	}
}
