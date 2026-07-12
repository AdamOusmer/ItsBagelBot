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
