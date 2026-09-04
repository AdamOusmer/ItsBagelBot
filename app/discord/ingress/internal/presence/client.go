// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package presence

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// RefreshInterval paces both the presence ticker and, transitively, how
// stale the displayed count can be. Discord allows 5 presence updates per 20
// seconds per session (a budget shared with nothing else here, since this is
// the only thing on this connection that calls Update Presence), and the
// enrolled-user count moves on the order of signups per day, not per minute
// -- a several-minute interval is nowhere near that budget and the number
// visibly moving faster would buy nothing. 5 minutes was picked as "long
// enough that this is clearly not the bottleneck on Discord's limit, short
// enough that a broadcaster watching the number climb after a launch post
// doesn't wait long for it to move" -- not derived from a measurement.
const RefreshInterval = 5 * time.Minute

// rpcTimeout bounds one counts.get round trip so a stalled users service
// cannot hang a presence refresh indefinitely; see Source.Refresh, which
// treats a timeout the same as any other Fetch error (log, skip, keep
// showing whatever was last sent).
const rpcTimeout = 3 * time.Second

// NewFetch builds the Fetch that calls the users service's narrow public
// counts RPC. subject is bagel.rpc.internal.users.counts.get by convention
// (see app/db/users/main.go's NATS_INTERNAL_USERS_COUNTS_SUBJECT); nc is a
// dedicated RPC connection (bus.Connect(bus.RPCURL(...))), not the fire-and-
// forget event publisher ingress already holds -- request/reply needs a
// connection that can receive a reply on its own inbox subject, which the
// pooled async Publisher does not expose.
func NewFetch(nc *nats.Conn, subject string) Fetch {
	return func(ctx context.Context) (int, error) {
		ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
		defer cancel()
		reply, err := bus.RequestJSON[usersrpc.CountsReply](ctx, nc, subject, usersrpc.CountsRequest{})
		if err != nil {
			return 0, err
		}
		if reply.Error != "" {
			return 0, errors.New(reply.Error)
		}
		return reply.TotalUsers, nil
	}
}
