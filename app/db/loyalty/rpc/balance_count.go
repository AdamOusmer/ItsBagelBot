// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
)

type standingsCountKey struct {
	Channel uint64
	Epoch   repository.FreezeID
	Minimum repository.BalanceThreshold
}

const standingsCountTTL = 30 * time.Second

func newStandingsCountCache() *cache.Keyed[standingsCountKey, int64] {
	return cache.NewKeyed[standingsCountKey, int64](cache.DefaultCapacity, standingsCountTTL, func(k standingsCountKey) string {
		raw, _ := codec.Marshal(k)
		return string(raw)
	})
}

func (l *loyaltyRPC) handleBalanceCount(ctx context.Context, req loyaltyrpc.BalanceCountRequest) loyaltyrpc.Reply {
	uid, err := bus.UserID(req.UserID)
	if err != nil || uid == 0 || req.MinPoints < 0 {
		return refuse(domainrpc.CodeInvalid, "invalid balance threshold or user_id")
	}
	route, err := l.resolveBoard(ctx, uid, loyaltyrpc.Request{})
	if err != nil {
		return l.fail("loyalty balance.count_at_least", err)
	}
	minimum := repository.BalanceThreshold{Points: req.MinPoints, WatchSeconds: req.MinWatchSeconds}
	n, err := l.countStandings(ctx, route, minimum)
	if err != nil {
		return l.fail("loyalty balance.count_at_least", err)
	}
	return loyaltyrpc.Reply{Total: n, Found: true, FreezeStamp: route.activeStamp()}
}

func (l *loyaltyRPC) countStandings(ctx context.Context, route boardRoute, minimum repository.BalanceThreshold) (int64, error) {
	epoch := route.state.FreezeID
	if epoch == "" {
		return l.repo.CountBalancesAtLeast(ctx, route.userID, epoch, minimum)
	}
	key := standingsCountKey{Channel: route.userID, Epoch: epoch, Minimum: minimum}
	return l.standingsCount.GetOrLoadTTL(ctx, key, func(ctx context.Context) (int64, time.Duration, error) {
		count, err := l.repo.CountBalancesAtLeast(ctx, key.Channel, key.Epoch, key.Minimum)
		ttl := max(time.Nanosecond, min(time.Until(route.state.ExpiresAt), standingsCountTTL-time.Nanosecond))
		return count, ttl, err
	})
}
