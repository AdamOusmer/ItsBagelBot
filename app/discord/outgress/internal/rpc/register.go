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

type Wiring struct {
	NC     *nats.Conn
	Prefix string
	Queue  string
	App    *newrelic.Application
	Log    *zap.Logger
}

type verb struct {
	Name    string
	Timeout time.Duration
}

func register[Req any, Resp any](wire Wiring, v verb, handle func(context.Context, Req) Resp) error {
	return bus.QueueSubscribeJSON[Req, Resp](
		wire.NC, wire.Prefix+"."+v.Name, wire.Queue, v.Timeout, wire.App, wire.Log, handle)
}
