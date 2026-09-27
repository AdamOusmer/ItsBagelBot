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

	"github.com/stretchr/testify/require"
)

func TestAccountAgeLookupCachesInSesame(t *testing.T) {
	var calls atomic.Int32
	a := &AccountAgeRPC{
		cache: cache.New[AccountAgeResult](100, time.Minute),
		request: func(_ context.Context, req outgressrpc.AccountAgeRequest) (outgressrpc.AccountAgeReply, error) {
			calls.Add(1)
			return outgressrpc.AccountAgeReply{
				TargetID: req.TargetID, UserFound: true,
				CreatedAt: time.Date(2016, time.June, 1, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	defer a.cache.Close()

	for range 2 {
		result, err := a.Lookup(context.Background(), "viewer", "")
		require.NoError(t, err)
		require.True(t, result.UserFound)
	}
	require.Equal(t, int32(1), calls.Load(), "the second command lookup must be served by Sesame's cache")
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
