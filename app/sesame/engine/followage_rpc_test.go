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

func TestFollowageLookupCachesInSesame(t *testing.T) {
	var calls atomic.Int32
	f := &FollowageRPC{
		cache: cache.New[FollowageResult](100, time.Minute),
		request: func(_ context.Context, req outgressrpc.FollowageRequest) (outgressrpc.FollowageReply, error) {
			calls.Add(1)
			return outgressrpc.FollowageReply{
				TargetID: req.TargetID, UserFound: true, Following: true,
				FollowedAt: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	defer f.cache.Close()

	for range 2 {
		result, err := f.Lookup(context.Background(), "channel", "viewer", "")
		require.NoError(t, err)
		require.True(t, result.Following)
	}
	require.Equal(t, int32(1), calls.Load(), "the second command lookup must be served by Sesame's cache")
}
