// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/pkg/bus"
)

type Wiring struct {
	bus.RPCWiring
	Repo *repository.Modules
}

const custodyBudget = 3 * time.Second
