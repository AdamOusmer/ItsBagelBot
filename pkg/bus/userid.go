// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/internal/domain/rpc"
)

// The two refusals every user-scoped RPC verb in the fleet can answer with.
// They are the wire strings: a caller reads them out of the reply's error
// field, so they are pinned here and nowhere else.
//
// Before this, sixteen handlers spelled the same guard by hand and had drifted
// onto three spellings of the same refusal ("invalid user_id",
// "user_id must be numeric", "user_id required"). "invalid user_id" won
// because it is what the shared projection guard already answered and what the
// majority of the hand-copied sites already said; no console code switches on
// any of the three, so the collapse is invisible to callers.
const (
	refusalNoUserID      = "bad request"
	refusalInvalidUserID = "invalid user_id"
)

// ErrNoUserID and ErrInvalidUserID are the same two refusals for the handlers
// that need the parsed id partway through their own logic rather than as a
// bind-time prologue (a username fallback, a reserved namespace id, an
// optional second id). Their messages are the wire strings above, so a site
// that echoes err.Error() into its reply says exactly what ServeForUser says.
var (
	ErrNoUserID      = errors.New(refusalNoUserID)
	ErrInvalidUserID = errors.New(refusalInvalidUserID)
)

// UserID parses the wire user id: the RPC contracts carry Twitch ids as
// strings because JSON numbers cannot hold a uint64 losslessly, so every
// user-scoped verb starts by converting one.
func UserID(raw string) (uint64, error) {
	if raw == "" {
		return 0, ErrNoUserID
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, ErrInvalidUserID
	}
	return id, nil
}

// Requesting is the only thing the guard reads off a request: the raw user id
// string. Satisfying an interface is cheaper than a per-call accessor closure
// or a second copy of the guard per request type.
type Requesting interface{ Requested() string }

// Failing is the only thing the guard writes onto a reply. Every user-scoped
// reply in the fleet carries an `error` field; the pointer-constraint form is
// what app/db/discord/rpc already uses for its refusals, so this follows the
// house shape instead of inventing a second one.
type Failing interface{ Failed(message string) }

// ForUser wraps one handler in the user-id guard: reject an empty id, reject
// one that is not a uint64, turn a load error into the reply's error field.
// It is the Template Method for user-scoped verbs -- the skeleton is fixed
// here and load is the single hook -- and exists separately from ServeForUser
// so a verb inside a ServeVerbs table can be guarded too, which a helper that
// only ever binds a subject could not do.
//
// load is given the request as well as the parsed id because most replies echo
// the raw user_id string back; deriving it from the uint64 would re-render it
// and lose a caller's zero padding.
func ForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](load func(context.Context, Req, uint64) (Rep, error)) func(context.Context, Req) Rep {
	return func(ctx context.Context, req Req) Rep {
		id, err := UserID(req.Requested())
		if err != nil {
			return RefuseErr[Rep, PR](err)
		}
		reply, err := load(ctx, req, id)
		if err != nil {
			return RefuseErr[Rep, PR](err)
		}
		return reply
	}
}

// ServeForUser binds one user-scoped subject: ForUser's guard plus Serve's
// wiring, which is what the overwhelming majority of these verbs want.
func ServeForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](w RPCWiring, subject string, load func(context.Context, Req, uint64) (Rep, error)) error {
	return Serve(w, subject, ForUser[Req, Rep, PR](load))
}

// VerbForUser names one guarded verb for a ServeVerbs table, the way At names
// an unguarded one.
func VerbForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](name string, load func(context.Context, Req, uint64) (Rep, error)) Verb[Req, Rep] {
	return Verb[Req, Rep]{Name: name, Handle: ForUser[Req, Rep, PR](load)}
}

// Refuse builds the zero reply carrying one refusal message. Exported because
// handlers that guard partway through their own logic still have to spell the
// same envelope.
func Refuse[Rep any, PR interface {
	*Rep
	Failing
}](message string) Rep {
	var zero Rep
	PR(&zero).Failed(message)
	return zero
}

// userIDRules maps the guard's own two sentinels onto the shared vocabulary.
// The guard rejects a request it can see is unusable before any store is
// touched, so both are CodeInvalid: retrying the same user_id cannot help.
var userIDRules = []rpc.Rule{
	rpc.Is(ErrNoUserID, rpc.CodeInvalid),
	rpc.Is(ErrInvalidUserID, rpc.CodeInvalid),
}

// RefuseErr is Refuse for a caller that still holds the error rather than a
// message: it writes the same sentence AND, when the reply carries a code
// field, the machine-readable code the console branches on.
//
// It is a second helper instead of a change to Refuse because Failing is
// message-only and a dozen replies outside the code vocabulary still
// implement exactly that; widening the contract would have meant editing all
// of them for no gain. A reply that has not adopted rpc.Refusal falls through
// to Failed and answers byte-identically to before.
func RefuseErr[Rep any, PR interface {
	*Rep
	Failing
}](err error) Rep {
	var zero Rep
	target := PR(&zero)
	refusal := rpc.Fail(err, userIDRules...)
	if coded, ok := any(target).(rpc.Refusing); ok {
		coded.Refuse(refusal)
		return zero
	}
	target.Failed(refusal.Error)
	return zero
}
