// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

// TwitchAccountLookup resolves current login ownership for point transfers.
// Age display caches must not send points to an account under an old login.
type TwitchAccountLookup interface {
	ResolveLogin(ctx context.Context, login string) (string, bool, error)
}

func (a *AccountAgeRPC) ResolveLogin(ctx context.Context, login string) (string, bool, error) {
	reply, err := a.request(ctx, outgressrpc.AccountAgeRequest{TargetLogin: login})
	if err != nil {
		return "", false, err
	}
	if reply.Error != "" {
		return "", false, errors.New(reply.Error)
	}
	return reply.TargetID, reply.UserFound, nil
}

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
