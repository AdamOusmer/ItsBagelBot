// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"strconv"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
)

func (r *Loyalty) SetInvalidation(nc *nats.Conn, prefix string) {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.nc, r.invalidationPrefix = nc, prefix
}

// Caller must hold r.persistMu.
func (r *Loyalty) publishEarned(earn map[balKey]*earnSum) {
	if r.nc == nil || r.nc.IsClosed() || r.invalidationPrefix == "" {
		return
	}
	viewers := map[uint64][]string{}
	for k := range earn {
		viewers[k.userID] = append(viewers[k.userID], strconv.FormatUint(k.viewerID, 10))
	}
	for userID, ids := range viewers {
		if err := invalidate.PublishKeys(r.nc, r.invalidationPrefix, loyaltyrpc.InvalidateScope, strconv.FormatUint(userID, 10), ids...); err != nil {
			r.log.Warn("loyalty: failed to broadcast earned invalidation", zap.Uint64("user_id", userID), zap.Error(err))
		}
	}
}
