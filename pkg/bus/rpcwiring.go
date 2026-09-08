// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// DefaultRPCTimeout bounds one handler when a wiring leaves Timeout unset.
// Two seconds is what every data service already passed by hand: their verbs
// are a single indexed lookup or one short transaction, so anything slower is
// a wedged pool and the caller is better off failing than queueing behind it.
const DefaultRPCTimeout = 2 * time.Second

// RPCWiring is the process-wide handle set every request/reply subscriber
// needs. It travels as one value because the positional form these calls used
// to take (nc, repo, subject, queueGroup, app, log) put two plain strings next
// to each other that are both plausible in either position, so a transposed
// subject and queue group compiled fine and bound the wrong thing.
//
// The repository is deliberately NOT a field here: it is the one handle whose
// type differs per service, and a type parameter for it would infect every
// helper that only ever touches the connection. Services that want the repo
// bundled embed this struct in their own Wiring and add the field (see
// app/db/users/rpc.Wiring), which keeps the concrete repo type and still lets
// them pass w.RPCWiring to the shared helpers.
type RPCWiring struct {
	NC    *nats.Conn
	App   *newrelic.Application
	Queue string
	Log   *zap.Logger
	// Timeout bounds one handler. Zero means DefaultRPCTimeout; set it only
	// where a verb is legitimately slower than a lookup (the notifications
	// janitor sweep, for one).
	Timeout time.Duration
}

func (w RPCWiring) timeout() time.Duration {
	if w.Timeout == 0 {
		return DefaultRPCTimeout
	}
	return w.Timeout
}

// Serve registers one verb on its full subject with the wiring every verb of a
// service shares.
func Serve[Req, Rep any](w RPCWiring, subject string, h func(context.Context, Req) Rep) error {
	return QueueSubscribeJSON[Req, Rep](w.NC, subject, w.Queue, w.timeout(), w.App, w.Log, h)
}

// Verb is one entry of a service's verb registry: the subject suffix and the
// handler that answers it.
type Verb[Req, Rep any] struct {
	Name   string
	Handle func(context.Context, Req) Rep
}

// At names one verb's handler. It exists so a verb table reads as a list of
// (name, handler) pairs with the request and reply types inferred from the
// handler, instead of repeating the pair of type arguments on every entry.
func At[Req, Rep any](name string, h func(context.Context, Req) Rep) Verb[Req, Rep] {
	return Verb[Req, Rep]{Name: name, Handle: h}
}

// ServeVerbs is the Registry: a service declares its verb table once and this
// binds every entry under prefix, instead of each service spelling out its own
// loop over an anonymous struct slice plus the seven-argument subscribe call.
// The first failure stops the registration and is returned; a half-bound
// service is the caller's cue to exit, which is what every caller does.
func ServeVerbs[Req, Rep any](w RPCWiring, prefix string, verbs ...Verb[Req, Rep]) error {
	for _, v := range verbs {
		if err := Serve(w, prefix+"."+v.Name, v.Handle); err != nil {
			return err
		}
	}
	return nil
}
