// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"time"

	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/pkg/bus"
)

// Wiring is everything the modules projection and dashboard surfaces need. It
// replaces the (nc, repo, subject, queueGroup, app, log) positional form,
// whose two adjacent strings were transposable without a compile error.
//
// The quotes, personality, Govee and Spotify surfaces share this wiring but
// not this struct: each hangs off a different store (a credential vault or the
// quote book), so it takes bus.RPCWiring plus its own repository rather than
// carrying four repo fields that one entry point each would read.
type Wiring struct {
	bus.RPCWiring
	Repo *repository.Modules
}

// custodyBudget bounds one credential-custody handler (Govee keys, Spotify
// tokens). Three seconds rather than the two-second wiring default because
// every write here seals or unseals through the keyset before it touches the
// row, and a sealed write that times out leaves the console reporting a
// failure for a key that was in fact stored.
const custodyBudget = 3 * time.Second
