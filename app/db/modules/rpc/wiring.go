// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/pkg/bus"
)

// Wiring is everything the modules projection and dashboard surfaces need. It
// replaces the (nc, repo, subject, queueGroup, app, log) positional form,
// whose two adjacent strings were transposable without a compile error.
//
// The quotes, personality, Govee and Spotify surfaces keep their own wiring
// values: they hang off a different store (a credential vault or the quote
// book), and two of them run on a deliberately longer handler budget.
type Wiring struct {
	bus.RPCWiring
	Repo *repository.Modules
}
