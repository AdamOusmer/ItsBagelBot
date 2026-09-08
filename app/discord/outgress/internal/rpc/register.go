// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Wiring is the connection half of a subscription: everything a registration
// needs that is the same for every verb in a service. Both RPC surfaces here
// take one -- SetupWiring embeds it and adds the two optional readers only the
// dashboard surface consults.
type Wiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	Log    *zap.Logger
}

// EngineWiring is Wiring under the name the engine-facing surface has always
// used. Kept as an alias rather than a second struct so main keeps composing
// one value for SubscribeEngine and SubscribeTickets.
type EngineWiring = Wiring

// verb is the per-subject half: the name appended to the prefix, and the
// deadline the handler runs under. The two travel together because they are
// the only things that differ between the registrations below, and pairing
// them is what lets one registrar cover all of them.
type verb struct {
	Name    string
	Timeout time.Duration
}

// register subscribes one verb. It exists because thirteen registrations had
// each spelled out the same argument vector -- connection, prefix+name, queue,
// timeout, APM app, logger, handler -- so a change to how this service answers
// an RPC meant thirteen identical edits, and one missed line would be a verb
// answering on a different queue group with nothing to flag it.
//
// The Req/Resp parameters stay explicit at every call site even though Go can
// infer them from the handler: the request and reply types ARE the wire
// contract, and a caller reading a list of registrations should see which
// contract each subject serves without opening the handler.
func register[Req any, Resp any](wire Wiring, v verb, handle func(context.Context, Req) Resp) error {
	return bus.QueueSubscribeJSON[Req, Resp](
		wire.NC, wire.Prefix+"."+v.Name, wire.Queue, v.Timeout, wire.App, wire.Log, handle)
}
