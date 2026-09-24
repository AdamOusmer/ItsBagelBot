// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumer

import (
	"context"

	"ItsBagelBot/pkg/bus"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type Handler func(*bus.Message) error

type Lanes struct {
	PremiumSubject  string
	StandardSubject string
}

type Config struct {
	Lanes          Lanes
	Policy         bus.ScalePolicy
	PremiumReserve int
}

type Consumer struct {
	sub   bus.Subscriber
	nrApp *newrelic.Application
	log   *zap.Logger
	cfg   Config
}

func New(sub bus.Subscriber, nrApp *newrelic.Application, cfg Config, log *zap.Logger) *Consumer {
	return &Consumer{sub: sub, nrApp: nrApp, log: log, cfg: cfg}
}

func (c *Consumer) Start(ctx context.Context, handle Handler) (*bus.Weighted, error) {
	return bus.ConsumeWeighted(ctx, c.nrApp, c.lanes(handle), c.cfg.Policy, c.log)
}

func (c *Consumer) lanes(handle Handler) []bus.WeightedLane {
	lanes := []bus.WeightedLane{
		{Sub: c.sub, Subject: c.cfg.Lanes.PremiumSubject, Handle: handle, Reserve: c.cfg.PremiumReserve},
		{Sub: c.sub, Subject: c.cfg.Lanes.StandardSubject, Handle: handle},
	}
	if !bus.FlowConsumeEnabled() {
		return lanes
	}
	for _, lane := range []string{c.cfg.Lanes.PremiumSubject, c.cfg.Lanes.StandardSubject} {
		lanes = append(lanes, bus.WeightedLane{
			Sub: c.sub, Subject: bus.RetryLaneSubject(lane), Handle: handle,
		})
	}
	return lanes
}
