// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package events

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

type conn interface {
	PublishMsg(m *nats.Msg) error
}

type Publisher struct {
	nc conn
}

var _ ports.Events = (*Publisher)(nil)

func New(nc *nats.Conn) *Publisher { return &Publisher{nc: nc} }

func (p *Publisher) Publish(_ context.Context, run *deploy.Run) error {
	data, err := codec.Marshal(run)
	if err != nil {
		return fmt.Errorf("encode run %s: %w", run.ID, err)
	}
	return p.nc.PublishMsg(&nats.Msg{Subject: deploy.EventsSubject(run.ID), Data: data})
}
