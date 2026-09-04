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

func TestUptimeLookupCachesInSesame(t *testing.T) {
	var calls atomic.Int32
	started := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	u := &UptimeRPC{
		cache: cache.New[UptimeResult](100, 30*time.Second),
		request: func(_ context.Context, req outgressrpc.UptimeRequest) (outgressrpc.UptimeReply, error) {
			calls.Add(1)
			return outgressrpc.UptimeReply{Live: true, StartedAt: started}, nil
		},
	}
	defer u.cache.Close()

	for range 2 {
		result, err := u.Lookup(context.Background(), "channel")
		require.NoError(t, err)
		require.True(t, result.Live)
		require.Equal(t, started, result.StartedAt)
	}
	require.Equal(t, int32(1), calls.Load(), "the second command lookup must be served by Sesame's cache")
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
