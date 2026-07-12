package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/nats-io/nats.go"
)

const (
	followageRPCTimeout  = 3500 * time.Millisecond
	followagePositiveTTL = 15 * time.Minute
	followageNegativeTTL = time.Minute

	// followageCacheCapacity ceilings the followage cache. It is keyed per
	// (broadcaster, viewer), so it grows with distinct viewers who run
	// !followage, not just broadcasters; it gets a larger ceiling than the
	// per-broadcaster caches but still well under the generic
	// cache.DefaultCapacity so viewer churn cannot pin ten thousand entries.
	followageCacheCapacity int64 = 8192
)

type FollowageResult struct {
	TargetID   string
	UserFound  bool
	Following  bool
	FollowedAt time.Time
}

type FollowageLookup interface {
	Lookup(ctx context.Context, broadcasterID, targetID, targetLogin string) (FollowageResult, error)
}

// FollowageRPC is Sesame's cached followage reader. Outgress supplies only the
// authenticated Twitch read; command freshness, singleflight and cache policy
// live here with the command runtime.
type FollowageRPC struct {
	cache   *cache.Cache[FollowageResult]
	request func(context.Context, outgressrpc.FollowageRequest) (outgressrpc.FollowageReply, error)
}

func NewFollowageRPC(nc *nats.Conn, prefix string) *FollowageRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".followage.get"
	return &FollowageRPC{
		cache: cache.New[FollowageResult](followageCacheCapacity, followageNegativeTTL),
		request: func(ctx context.Context, req outgressrpc.FollowageRequest) (outgressrpc.FollowageReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.FollowageReply](ctx, nc, subject, req, followageRPCTimeout)
		},
	}
}

func (f *FollowageRPC) Lookup(ctx context.Context, broadcasterID, targetID, targetLogin string) (FollowageResult, error) {
	keyTarget := targetID
	if keyTarget == "" {
		keyTarget = "login:" + strings.ToLower(strings.TrimPrefix(targetLogin, "@"))
	}
	return f.cache.GetOrLoadTTL(ctx, broadcasterID+":"+keyTarget, func(ctx context.Context) (FollowageResult, time.Duration, error) {
		reply, err := f.request(ctx, outgressrpc.FollowageRequest{
			BroadcasterID: broadcasterID, TargetID: targetID, TargetLogin: targetLogin,
		})
		if err != nil {
			return FollowageResult{}, 0, err
		}
		if reply.Error != "" {
			return FollowageResult{}, 0, errors.New(reply.Error)
		}
		result := FollowageResult{
			TargetID: reply.TargetID, UserFound: reply.UserFound,
			Following: reply.Following, FollowedAt: reply.FollowedAt,
		}
		ttl := followageNegativeTTL
		if result.Following {
			ttl = followagePositiveTTL
		}
		return result, ttl, nil
	})
}
