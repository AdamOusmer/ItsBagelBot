// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"strconv"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
)

type Wiring struct {
	bus.RPCWiring
	Repo *repository.Notifications
}

const (
	readBudget    = 3 * time.Second
	sendBudget    = 5 * time.Second
	cleanupBudget = 30 * time.Second
)

func accountKey(accountID uint64) string { return strconv.FormatUint(accountID, 10) }

func (w Wiring) invalidateNotifications(prefix, broadcasterID string) {
	if err := invalidate.Publish(w.NC, prefix, "notifications", broadcasterID); err != nil {
		w.Log.Warn("notification invalidation publish failed", zap.Error(err))
	}
}
