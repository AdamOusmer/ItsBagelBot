// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/invalidate"
)

func (r *Loyalty) SetInvalidations(h *bus.Invalidations) {
	r.invalidations.Store(h)
}

func (r *Loyalty) SetBalanceCache(client valkey_go.Client) {
	r.balanceCache = &pkg_valkey.LoyaltyCache{Client: client}
}

func (r *Loyalty) publishBalanceChange(userID uint64, viewers ...uint64) {
	keys := make([]string, len(viewers))
	for i, viewer := range viewers {
		keys[i] = strconv.FormatUint(viewer, 10)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.balanceCache.Drop(ctx, cache.Event{ID: userID, Keys: keys}); err != nil {
		r.log.Warn("loyalty: failed to drop cached balances", zap.Uint64("user_id", userID), zap.Error(err))
	}
	r.publishChange(invalidate.ScopeLoyalty, userID, keys...)
}

func (r *Loyalty) publishChange(scope invalidate.Scope, userID uint64, keys ...string) {
	h := r.invalidations.Load()
	if h == nil {
		return
	}
	_ = h.Publish(scope, userID, keys...)
}

func (r *Loyalty) publishEarned(userID uint64, entries []data.LoyaltyEarnEntry) {
	viewers := make(map[uint64]struct{}, len(entries))
	for _, entry := range entries {
		if earnsAnything(entry) {
			viewers[entry.ViewerID] = struct{}{}
		}
	}
	ids := make([]uint64, 0, len(viewers))
	for viewer := range viewers {
		ids = append(ids, viewer)
	}
	if len(ids) > 0 {
		r.publishBalanceChange(userID, ids...)
	}
}

func (r *Loyalty) publishAccountChange(userID uint64) {
	r.publishBalanceChange(userID)
	r.publishChange(invalidate.ScopeLoyaltyCounters, userID)
}
