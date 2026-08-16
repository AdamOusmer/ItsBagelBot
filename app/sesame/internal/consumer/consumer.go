// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

// Package consumer is sesame's ingress side: a single autoscaling consumer that
// drains the premium and standard lanes and hands every message off to a pipeline
// routine.
//
// The flow is NATS -> bus.ConsumeWeighted -> pool of pipeline routines. Both lanes
// feed one shared routine pool that grows under load and shrinks when calm. The
// premium lane reserves a slice of the pool (PremiumReserve) so a standard flood
// can never starve premium broadcasters. The consumer does no processing of its
// own; each in-flight message is run on a pool routine by the handler it was given
// (the engine pipeline).
//
// Both lanes are hot ingress subjects, so the subscriber binds them to
// receipt-level flow-controlled consumers by default: a handler's Ack releases
// no per-message acknowledgement, and a handler error schedules the event onto
// its retry lane instead of NACKing it. Handlers must therefore stay idempotent
// and must not rely on ordering. Because that retry arrives on a different
// subject, the consumer drains the two retry lanes as well — on the ordinary
// explicit-ACK path, since they are low-rate and a lost retry has no second
// chance. NATS_CONSUME_FLOW=off restores the explicit-ACK path on the hot lanes
// and drops the retry lanes with it, because nothing schedules onto them.
package consumer

import (
	"context"

	"ItsBagelBot/pkg/bus"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Handler is the per-message routine the consumer hands each event to. The engine
// pipeline's Process satisfies it.
type Handler func(*bus.Message) error

// Lanes names the two ingress subjects the consumer drains.
type Lanes struct {
	PremiumSubject  string
	StandardSubject string
}

// Config bundles the consumer's tuning: the two lanes, the autoscaler policy, and
// the percentage of the pool the premium lane reserves.
type Config struct {
	Lanes          Lanes
	Policy         bus.ScalePolicy
	PremiumReserve int
}

// Consumer drains the premium and standard lanes into one autoscaling pool.
type Consumer struct {
	sub   bus.Subscriber
	nrApp *newrelic.Application
	log   *zap.Logger
	cfg   Config
}

// New builds a Consumer over one subscriber.
func New(sub bus.Subscriber, nrApp *newrelic.Application, cfg Config, log *zap.Logger) *Consumer {
	return &Consumer{sub: sub, nrApp: nrApp, log: log, cfg: cfg}
}

// Start binds both lanes to handle and begins draining. It returns once the first
// consumer is running; the pool and autoscaler stop when ctx is cancelled. The
// returned *bus.Weighted lets the caller drain in-flight handlers on shutdown.
func (c *Consumer) Start(ctx context.Context, handle Handler) (*bus.Weighted, error) {
	return bus.ConsumeWeighted(ctx, c.nrApp, c.lanes(handle), c.cfg.Policy, c.log)
}

// lanes builds the subscription set: the two hot lanes always, plus the retry
// lane each of them schedules failures onto while receipt-level flow control is
// the active contract. The retry lanes carry no reserve — they are exceptional
// traffic and must not hold slots away from live chat.
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
