// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/pkg/bus"
)

type Wiring struct {
	bus.RPCWiring
	Commands *repository.Commands
	Fetches  *repository.Fetches
}
