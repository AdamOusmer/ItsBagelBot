// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRPCLookupsAreServedFromSesameCacheOnRepeat(t *testing.T) {
	cases := []struct {
		name   string
		lookup func(calls *atomic.Int32) (func(context.Context) (bool, error), func())
	}{
		{
			name: "followage",
			lookup: func(calls *atomic.Int32) (func(context.Context) (bool, error), func()) {
				f := &FollowageRPC{
					cache: cache.New[FollowageResult](100, time.Minute),
					request: func(_ context.Context, req outgressrpc.FollowageRequest) (outgressrpc.FollowageReply, error) {
						calls.Add(1)
						return outgressrpc.FollowageReply{TargetID: req.TargetID, UserFound: true, Following: true}, nil
					},
				}
				return func(ctx context.Context) (bool, error) {
					result, err := f.Lookup(ctx, FollowageQuery{BroadcasterID: "channel", TargetID: "viewer"})
					return result.Following, err
				}, f.cache.Close
			},
		},
		{
			name: "account age",
			lookup: func(calls *atomic.Int32) (func(context.Context) (bool, error), func()) {
				a := &AccountAgeRPC{
					cache: cache.New[AccountAgeResult](100, time.Minute),
					request: func(_ context.Context, req outgressrpc.AccountAgeRequest) (outgressrpc.AccountAgeReply, error) {
						calls.Add(1)
						return outgressrpc.AccountAgeReply{TargetID: req.TargetID, UserFound: true}, nil
					},
				}
				return func(ctx context.Context) (bool, error) {
					result, err := a.Lookup(ctx, "viewer", "")
					return result.UserFound, err
				}, a.cache.Close
			},
		},
		{
			name: "uptime",
			lookup: func(calls *atomic.Int32) (func(context.Context) (bool, error), func()) {
				u := &UptimeRPC{
					cache: cache.New[UptimeResult](100, 30*time.Second),
					request: func(context.Context, outgressrpc.UptimeRequest) (outgressrpc.UptimeReply, error) {
						calls.Add(1)
						return outgressrpc.UptimeReply{Live: true, StartedAt: time.Now()}, nil
					},
				}
				return func(ctx context.Context) (bool, error) {
					result, err := u.Lookup(ctx, "channel")
					return result.Live, err
				}, u.cache.Close
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			lookup, closeCache := tc.lookup(&calls)
			defer closeCache()

			for range 2 {
				found, err := lookup(context.Background())
				require.NoError(t, err)
				assert.True(t, found)
			}

			assert.Equal(t, int32(1), calls.Load(), "the second command lookup must be served by Sesame's cache")
		})
	}
}

func TestUptimeLookupSurfacesRPCError(t *testing.T) {
	u := &UptimeRPC{
		cache: cache.New[UptimeResult](100, 30*time.Second),
		request: func(_ context.Context, _ outgressrpc.UptimeRequest) (outgressrpc.UptimeReply, error) {
			return outgressrpc.UptimeReply{Error: "lookup failed"}, nil
		},
	}
	defer u.cache.Close()

	_, err := u.Lookup(context.Background(), "channel")
	require.Error(t, err)
}

func TestResolveLoginBypassesAccountAgeCache(t *testing.T) {
	var calls int
	a := &AccountAgeRPC{
		cache: cache.New[AccountAgeResult](100, time.Minute),
		request: func(_ context.Context, req outgressrpc.AccountAgeRequest) (outgressrpc.AccountAgeReply, error) {
			calls++
			require.Equal(t, "alice", req.TargetLogin)
			if calls == 1 {
				return outgressrpc.AccountAgeReply{TargetID: "8", UserFound: true}, nil
			}
			return outgressrpc.AccountAgeReply{UserFound: false}, nil
		},
	}
	defer a.cache.Close()
	cached, err := a.Lookup(context.Background(), "", "alice")
	require.NoError(t, err)
	require.True(t, cached.UserFound)
	id, found, err := a.ResolveLogin(context.Background(), "alice")
	require.NoError(t, err)
	require.False(t, found, "a renamed account must not receive a transfer through its old login")
	require.Empty(t, id)
	require.Equal(t, 2, calls)
}
