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
	accountAgeRPCTimeout  = 3500 * time.Millisecond
	accountAgePositiveTTL = time.Hour
	accountAgeNegativeTTL = time.Minute
)

type AccountAgeResult struct {
	TargetID  string
	UserFound bool
	CreatedAt time.Time
}

type AccountAgeLookup interface {
	Lookup(ctx context.Context, targetID, targetLogin string) (AccountAgeResult, error)
}

// AccountAgeRPC is Sesame's cached account-age reader. Outgress supplies only
// the authenticated Twitch read; command freshness, singleflight and cache
// policy live here with the command runtime. A Twitch account's creation date
// never changes, so a hit stays valid for the full positive TTL.
type AccountAgeRPC struct {
	cache   *cache.Cache[AccountAgeResult]
	request func(context.Context, outgressrpc.AccountAgeRequest) (outgressrpc.AccountAgeReply, error)
}

func NewAccountAgeRPC(nc *nats.Conn, prefix string) *AccountAgeRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".accountage.get"
	return &AccountAgeRPC{
		cache: cache.New[AccountAgeResult](cache.DefaultCapacity, accountAgeNegativeTTL),
		request: func(ctx context.Context, req outgressrpc.AccountAgeRequest) (outgressrpc.AccountAgeReply, error) {
			return bus.RequestJSONTimeout[outgressrpc.AccountAgeReply](ctx, nc, subject, req, accountAgeRPCTimeout)
		},
	}
}

func (a *AccountAgeRPC) Lookup(ctx context.Context, targetID, targetLogin string) (AccountAgeResult, error) {
	keyTarget := targetID
	if keyTarget == "" {
		keyTarget = "login:" + strings.ToLower(strings.TrimPrefix(targetLogin, "@"))
	}
	return a.cache.GetOrLoadTTL(ctx, keyTarget, func(ctx context.Context) (AccountAgeResult, time.Duration, error) {
		reply, err := a.request(ctx, outgressrpc.AccountAgeRequest{TargetID: targetID, TargetLogin: targetLogin})
		if err != nil {
			return AccountAgeResult{}, 0, err
		}
		if reply.Error != "" {
			return AccountAgeResult{}, 0, errors.New(reply.Error)
		}
		result := AccountAgeResult{
			TargetID: reply.TargetID, UserFound: reply.UserFound, CreatedAt: reply.CreatedAt,
		}
		ttl := accountAgeNegativeTTL
		if result.UserFound {
			ttl = accountAgePositiveTTL
		}
		return result, ttl, nil
	})
}
