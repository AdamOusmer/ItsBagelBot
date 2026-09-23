// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package events publishes full run snapshots on bagel.deploy.events.<runId>.
//
// Snapshots, not deltas: the console's SSE stream forwards each one as a
// frame and keeps the highest Seq, so a dropped or reordered message costs
// nothing once the next one lands. Core publish, not JetStream: DEPLOY_RUNS
// already holds the durable copy, and a stream reconnect starts from a get.
package events

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

// conn is the one *nats.Conn method the publisher needs.
type conn interface {
	PublishMsg(m *nats.Msg) error
}

// Publisher implements ports.Events.
type Publisher struct {
	nc conn
}

var _ ports.Events = (*Publisher)(nil)

// New binds the publisher to the RPC-plane connection, the plane the admin
// console's stream subscription is on.
func New(nc *nats.Conn) *Publisher { return &Publisher{nc: nc} }

func (p *Publisher) Publish(_ context.Context, run *deploy.Run) error {
	data, err := codec.Marshal(run)
	if err != nil {
		return fmt.Errorf("encode run %s: %w", run.ID, err)
	}
	return p.nc.PublishMsg(&nats.Msg{Subject: deploy.EventsSubject(run.ID), Data: data})
}
