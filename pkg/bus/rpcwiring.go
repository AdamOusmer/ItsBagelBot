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

const DefaultRPCTimeout = 2 * time.Second

type RPCWiring struct {
	NC      *nats.Conn
	App     *newrelic.Application
	Queue   string
	Log     *zap.Logger
	Timeout time.Duration
	Pool    RPCPoolPolicy
}

func (w RPCWiring) Within(d time.Duration) RPCWiring {
	w.Timeout = d
	return w
}

// Serially is for handlers that read-modify-write in-process or check-then-act without a DB guard.
func (w RPCWiring) Serially() RPCWiring {
	w.Pool = RPCPoolPolicy{MaxWorkers: 1}
	return w
}

func (w RPCWiring) timeout() time.Duration {
	if w.Timeout == 0 {
		return DefaultRPCTimeout
	}
	return w.Timeout
}

func (w RPCWiring) subscription(subject string) RPCSubscription {
	return RPCSubscription{Subject: subject, QueueGroup: w.Queue, Policy: w.Pool}
}

func Serve[Req, Rep any](w RPCWiring, subject string, h func(context.Context, Req) Rep) error {
	return QueueSubscribeJSONConcurrent[Req, Rep](w.NC, w.subscription(subject), w.timeout(), w.App, w.Log, h)
}

// ServeMsg is Serve for handlers that decode and respond to the raw message themselves.
func ServeMsg(w RPCWiring, subject string, h func(context.Context, *nats.Msg)) error {
	_, err := QueueSubscribeRPCConcurrent(w.NC, w.subscription(subject), msgRPCHandler(subject, w.timeout(), w.App, h))
	return flushedSubscription(w.NC, subject, err)
}

type Verb[Req, Rep any] struct {
	Name   string
	Handle func(context.Context, Req) Rep
}

func At[Req, Rep any](name string, h func(context.Context, Req) Rep) Verb[Req, Rep] {
	return Verb[Req, Rep]{Name: name, Handle: h}
}

func ServeVerbs[Req, Rep any](w RPCWiring, prefix string, verbs ...Verb[Req, Rep]) error {
	for _, v := range verbs {
		if err := Serve(w, prefix+"."+v.Name, v.Handle); err != nil {
			return err
		}
	}
	return nil
}
