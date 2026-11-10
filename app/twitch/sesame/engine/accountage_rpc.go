// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
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
	reply, err := a.rpc.request(ctx, outgressrpc.AccountAgeRequest{TargetLogin: login})
	if err != nil {
		return "", false, err
	}
	if reply.Error != "" {
		return "", false, errors.New(reply.Error)
	}
	return reply.TargetID, reply.UserFound, nil
}

type AccountAgeRPC struct {
	rpc *cachedRPC[outgressrpc.AccountAgeRequest, outgressrpc.AccountAgeReply, AccountAgeResult]
}

func NewAccountAgeRPC(nc *nats.Conn, prefix string) *AccountAgeRPC {
	return &AccountAgeRPC{rpc: newBusRPC[outgressrpc.AccountAgeRequest](nc, rpcEndpoint{
		subject:  rpcSubject(prefix, "accountage"),
		timeout:  accountAgeRPCTimeout,
		capacity: cache.DefaultCapacity,
		missTTL:  accountAgeNegativeTTL,
	}, decodeAccountAge)}
}

func decodeAccountAge(reply outgressrpc.AccountAgeReply) (AccountAgeResult, time.Duration, string) {
	result := AccountAgeResult{TargetID: reply.TargetID, UserFound: reply.UserFound, CreatedAt: reply.CreatedAt}
	return result, ttlWhen(result.UserFound, accountAgePositiveTTL, accountAgeNegativeTTL), reply.Error
}

func (a *AccountAgeRPC) Lookup(ctx context.Context, targetID, targetLogin string) (AccountAgeResult, error) {
	return a.rpc.lookup(ctx, targetCacheKey(targetID, targetLogin), outgressrpc.AccountAgeRequest{TargetID: targetID, TargetLogin: targetLogin})
}
