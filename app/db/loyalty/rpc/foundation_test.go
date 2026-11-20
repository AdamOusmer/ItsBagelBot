// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/internal/watchtime"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/require"
)

func TestBalanceCreditRPC(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	require.NoError(t, h.repo.EnsureWatchSchema(ctx))
	require.NoError(t, h.repo.EnsureBalanceCreditSchema(ctx))
	require.NoError(t, h.repo.RestoreUser(ctx, 2, 11))
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, tc := range []struct {
		name            string
		viewer          uint64
		key             string
		value, instance int64
		replayed        bool
		code            domainrpc.Code
	}{
		{name: "first credit", viewer: 9, key: "raffle:r:1", value: 8, instance: 11},
		{name: "replay", viewer: 9, key: "raffle:r:1", value: 8, instance: 11, replayed: true},
		{name: "conflict", viewer: 9, key: "raffle:r:1", value: 3, instance: 11, code: domainrpc.CodeConflict},
		{name: "different account", viewer: 9, key: "raffle:r:2", value: 8, instance: 10, code: domainrpc.CodeForbidden},
		{name: "zero instance with unused key", viewer: 9, key: "raffle:r:3", value: 8, code: domainrpc.CodeForbidden},
		{name: "missing viewer", key: "raffle:r:3", value: 8, code: domainrpc.CodeInvalid},
		{name: "missing key", viewer: 9, value: 8, code: domainrpc.CodeInvalid},
		{name: "unprefixed key", viewer: 9, key: "key", value: 8, code: domainrpc.CodeInvalid},
		{name: "invalid value", viewer: 9, key: "raffle:r:4", value: -1, code: domainrpc.CodeInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.BalanceCredit(t.Context(), engine.PointCredit{BroadcasterID: 2, ViewerID: tc.viewer,
				ViewerLogin: "winner", Value: tc.value, Key: tc.key, AccountCreatedAt: tc.instance})
			if tc.code != domainrpc.CodeOK {
				requireRPCCode(t, err, tc.code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, engine.CreditOutcome{Balance: *balanceOf("9", "winner", 8), Replayed: tc.replayed}, got)
		})
	}
	h.requireStored(t, map[uint64]int64{9: 8})
}

func requireRPCCode(t *testing.T, err error, want domainrpc.Code) {
	t.Helper()
	var failure bus.RPCReplyError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, want, failure.Code)
}

func TestBalanceCreditRPCStorageUnavailable(t *testing.T) {
	h := newHarness(t)
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	_, err := client.BalanceCredit(t.Context(), engine.PointCredit{BroadcasterID: 2, ViewerID: 9, Value: 8, Key: "raffle:r:1"})
	requireRPCCode(t, err, domainrpc.CodeUnavailable)
}

func TestBalanceCreditRPCRequiresAccountInstance(t *testing.T) {
	h := newHarness(t)
	reply := h.call(t, loyaltyrpc.VerbBalanceCredit, loyaltyrpc.Request{UserID: "2", ViewerID: "9", Value: 8, Key: "raffle:r:1"})
	require.Equal(t, domainrpc.Refused(domainrpc.CodeInvalid, "account_created_at is required"), reply.Refusal)
}

func TestBalanceCreditRPCPublishesOnReplay(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	require.NoError(t, h.repo.EnsureWatchSchema(ctx))
	require.NoError(t, h.repo.EnsureBalanceCreditSchema(ctx))
	h.repo.SetInvalidations(bus.NewInvalidations(h.nc, invalidationPrefix, nil))
	sub, err := h.nc.SubscribeSync(invalidationPrefix + "." + loyaltyrpc.InvalidateScope)
	require.NoError(t, err)
	request := loyaltyrpc.Request{UserID: "2", ViewerID: "9", Value: 8, Key: "raffle:r:1", AccountCreatedAt: new(int64)}
	for _, replay := range []bool{false, true} {
		reply := h.call(t, loyaltyrpc.VerbBalanceCredit, request)
		require.Equal(t, domainrpc.Refusal{}, reply.Refusal)
		require.Equal(t, replay, reply.Replayed)

		msg, err := sub.NextMsg(time.Second)
		require.NoError(t, err)
		var event invalidate.DTO
		require.NoError(t, codec.Unmarshal(msg.Data, &event))
		require.Equal(t, invalidate.DTO{BroadcasterID: "2", Keys: []string{"9"}}, event)
	}
}

func TestBalanceSnapshotRPC(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.client.Balance.Update().SetWatchSeconds(600).Exec(t.Context()))
	client := engine.NewLoyaltyRPC(h.nc, "loyalty")
	for _, tc := range []struct {
		viewer uint64
		want   loyaltyrpc.BalanceSnapshot
	}{{7, loyaltyrpc.BalanceSnapshot{Points: 1000, WatchSeconds: 600, Found: true}}, {9, loyaltyrpc.BalanceSnapshot{}}} {
		got, err := client.BalanceSnapshot(t.Context(), engine.BalanceSnapshotRequest{BroadcasterID: 2, ViewerID: tc.viewer})
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

func TestBalanceSnapshotRPCStorageUnavailable(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.client.Close())
	_, err := engine.NewLoyaltyRPC(h.nc, "loyalty").BalanceSnapshot(t.Context(), engine.BalanceSnapshotRequest{BroadcasterID: 2, ViewerID: 7})
	requireRPCCode(t, err, domainrpc.CodeUnavailable)
}

func TestSubscribeRequiresWatchStatus(t *testing.T) {
	require.ErrorContains(t, Subscribe(Wiring{}, "loyalty"), "WatchStatus")
}

func TestWatchStatusRPC(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status watchtime.BacklogStatus
		err    error
		want   loyaltyrpc.WatchStatus
	}{
		{name: "draining", status: watchtime.BacklogStatus{OldestPendingAge: time.Second, Healthy: true}, want: loyaltyrpc.WatchStatus{OldestPendingAgeMS: 1000, Healthy: true}},
		{name: "backlogged", status: watchtime.BacklogStatus{OldestPendingAge: 6 * time.Minute}, want: loyaltyrpc.WatchStatus{OldestPendingAgeMS: 360000}},
		{name: "outbox unavailable", err: errors.New("unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessWithWatchStatus(t, func(context.Context) (watchtime.BacklogStatus, error) { return tc.status, tc.err })
			got, err := engine.NewLoyaltyRPC(h.nc, "loyalty").WatchStatus(t.Context())
			if tc.err != nil {
				requireRPCCode(t, err, domainrpc.CodeUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
