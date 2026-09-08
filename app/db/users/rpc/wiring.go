// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/pkg/bus"
)

// Wiring bundles the process-wide handles every Subscribe* verb group needs.
// The connection, New Relic app, queue group and logger are the shared set
// (bus.RPCWiring, embedded so they can be handed straight to the shared
// subscribe helpers); the users repository is this service's own. Per-surface
// subjects and prefixes stay explicit arguments so each subscriber still
// declares the subjects it owns.
//
// adminauth is the exception: it holds the ent client directly (not the repo),
// so it reads the shared handles from here and takes its *ent.Client
// separately.
type Wiring struct {
	bus.RPCWiring
	Repo *repository.Users
}
