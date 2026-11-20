// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"

	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
)

func (l *loyaltyRPC) handleBalanceCount(ctx context.Context, req loyaltyrpc.BalanceCountRequest) loyaltyrpc.BalanceCount {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.BalanceCount{Refusal: domainrpc.Refused(domainrpc.CodeInvalid, err.Error())}
	}
	route, err := l.resolveBoard(ctx, userID, loyaltyrpc.Request{})
	if err != nil {
		return l.balanceCountFailure(err)
	}
	mins := repository.BalanceMinimums{Points: req.MinPoints, WatchSeconds: req.MinWatchSeconds}
	count, err := l.repo.BalanceCountAtLeast(ctx, userID, route.state.FreezeID, mins)
	if err != nil {
		return l.balanceCountFailure(err)
	}
	return loyaltyrpc.BalanceCount{Count: count, FreezeStamp: route.activeStamp()}
}

func (l *loyaltyRPC) balanceCountFailure(err error) loyaltyrpc.BalanceCount {
	return loyaltyrpc.BalanceCount{Refusal: l.fail("loyalty balance.count_at_least", err).Refusal}
}
