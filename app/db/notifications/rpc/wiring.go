// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/pkg/bus"
)

// Wiring is everything the three subscribe entry points share. It replaces the
// (nc, repo, cfg, app, log) positional form, whose adjacent handles were
// transposable without a compile error, and carries the queue group that used
// to be repeated in AdminConfig and UserConfig.
type Wiring struct {
	bus.RPCWiring
	Repo *repository.Notifications
}

// The three handler budgets this service runs on. None of them is the wiring
// default: these verbs are not the single indexed lookup the default is sized
// for, so each surface names the budget it needs through RPCWiring.Within.
//
// Pinned by TestHandlerBudgets: a change here has to be deliberate, not a side
// effect of touching the wiring.
const (
	// readBudget covers the verbs that page a list or flip read state. Three
	// seconds rather than the two-second default because the user list joins
	// the per-user read rows before it can answer.
	readBudget = 3 * time.Second
	// sendBudget covers admin send, the one verb that makes a cross-service
	// NATS hop (username -> id against the users service, itself allowed 3s)
	// before it writes, so it cannot fit in the read budget.
	sendBudget = 5 * time.Second
	// cleanupBudget covers the cron-driven janitor sweep, which deletes every
	// expired row in one statement. Thirty seconds is a table scan's budget,
	// not a request's; it is safe only because the subject is unexported from
	// the account and the queue group runs exactly one replica per tick.
	cleanupBudget = 30 * time.Second
)
