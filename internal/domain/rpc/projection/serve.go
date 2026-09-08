// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"

	"ItsBagelBot/pkg/bus"
)

// Requesting is the only thing the guard chain reads off a projection request:
// the raw user id string. fetchkey.FetchListRequest carries the same field
// under its own type, and satisfying an interface is cheaper than a second
// copy of the chain or a per-call accessor closure.
type Requesting interface{ Requested() string }

// Failing is the only thing the guard chain writes onto a reply. Every
// projection reply carries an `error` field; the pointer-constraint form is
// what app/db/discord/rpc already uses for its refusals, so this file follows
// the house shape instead of inventing a second one.
type Failing interface{ Failed(message string) }

// Requested satisfies Requesting for the shared projection request.
func (r Request) Requested() string { return r.UserID }

// Failed satisfies Failing for the users projection reply.
func (r *UserReply) Failed(message string) { r.Error = message }

// Failed satisfies Failing for the commands projection reply.
func (r *CommandsReply) Failed(message string) { r.Error = message }

// Failed satisfies Failing for the modules projection reply.
func (r *ModulesReply) Failed(message string) { r.Error = message }

// ServeProjection binds one projection subject. It is the Template Method for
// this surface: the skeleton -- reject an empty user id, reject one that is
// not a uint64, turn a repository error into the reply's error field -- is
// fixed here, and load is the single hook each service fills in.
//
// The three data services used to carry a private projectionRPC struct plus
// this identical chain, differing only in which repository method they called
// and which reply type they filled. Keeping the guard chain in one place also
// keeps the wire error strings ("bad request", "invalid user_id") from drifting
// apart, which callers switch on.
//
// load is given the request as well as the parsed id because every reply
// echoes the raw user_id string back; deriving it from the uint64 would
// re-render it and lose a caller's zero padding.
func ServeProjection[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](w bus.RPCWiring, subject string, load func(context.Context, Req, uint64) (Rep, error)) error {
	return bus.Serve(w, subject, func(ctx context.Context, req Req) Rep {
		return guard[Req, Rep, PR](ctx, req, load)
	})
}

func guard[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](ctx context.Context, req Req, load func(context.Context, Req, uint64) (Rep, error)) Rep {
	if req.Requested() == "" {
		return refuse[Rep, PR]("bad request")
	}

	id, err := strconv.ParseUint(req.Requested(), 10, 64)
	if err != nil {
		return refuse[Rep, PR]("invalid user_id")
	}

	reply, err := load(ctx, req, id)
	if err != nil {
		return refuse[Rep, PR](err.Error())
	}
	return reply
}

func refuse[Rep any, PR interface {
	*Rep
	Failing
}](message string) Rep {
	var zero Rep
	PR(&zero).Failed(message)
	return zero
}
