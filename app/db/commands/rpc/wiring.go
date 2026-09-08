// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/pkg/bus"
)

// Wiring is everything the commands RPC surfaces need. It replaces three
// shapes that said the same thing: two per-surface structs (the fetch
// dashboard's and the fetch key's) and three Subscribe functions taking
// (nc, repo, subject, queueGroup, app, log) positionally, where the two
// adjacent strings were transposable without a compile error.
//
// Both repositories ride along because main opens both and the surfaces split
// across them: the command verbs need Commands, the $(urlfetch) verbs need
// Fetches, and neither wants the other's handle passed at every call site.
type Wiring struct {
	bus.RPCWiring
	Commands *repository.Commands
	Fetches  *repository.Fetches
}
