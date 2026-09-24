// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/notifications/repository"
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
