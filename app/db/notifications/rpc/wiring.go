// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
)

type Wiring struct {
	bus.RPCWiring
	Repo          *repository.Notifications
	Invalidations *bus.Invalidations
}

func (w Wiring) invalidateDeleted(deleted repository.DeletedNotifications) {
	ids := make(map[uint64]bool)
	for _, target := range deleted.TargetUserIDs {
		if target == nil {
			w.Invalidations.PublishAll(invalidate.ScopeNotifications)
			return
		}
		ids[*target] = true
	}
	for id := range ids {
		w.Invalidations.Publish(invalidate.ScopeNotifications, id)
	}
}

const (
	readBudget    = 3 * time.Second
	sendBudget    = 5 * time.Second
	cleanupBudget = 30 * time.Second
)
