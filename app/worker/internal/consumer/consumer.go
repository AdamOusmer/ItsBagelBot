// Package consumer is the worker's ingress side: a single autoscaling consumer
// that drains the premium and standard lanes and hands every message off to a
// pipeline routine.
//
// The flow is NATS -> bus.ConsumeWeighted -> pool of pipeline routines. Both
// lanes feed one shared routine pool that grows under load and shrinks when
// calm (see bus.ConsumeWeighted). The premium lane reserves a slice of the pool
// (PremiumReserve) so a standard flood can never starve premium broadcasters.
// The consumer does no processing of its own; each in-flight message is run on
// a pool routine by the handler it was given (the pipeline).
package consumer

import (
	"context"

	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Handler is the per-message routine the consumer hands each event to. The
// pipeline's Process satisfies it.
type Handler func(*message.Message) error

// Lanes names the two ingress subjects the consumer drains.
type Lanes struct {
	PremiumSubject  string
	StandardSubject string
}

// Consumer drains the premium and standard lanes into one autoscaling pool.
type Consumer struct {
	sub            message.Subscriber
	nrApp          *newrelic.Application
	log            *zap.Logger
	lanes          Lanes
	policy         bus.ScalePolicy
	premiumReserve int
}

// New builds a Consumer over one subscriber. lanes are the two ingress
// subjects, policy bounds the autoscaler, and premiumReserve is the percentage
// of the pool kept for the premium lane.
func New(sub message.Subscriber, nrApp *newrelic.Application, lanes Lanes, policy bus.ScalePolicy, premiumReserve int, log *zap.Logger) *Consumer {
	return &Consumer{
		sub:            sub,
		nrApp:          nrApp,
		log:            log,
		lanes:          lanes,
		policy:         policy,
		premiumReserve: premiumReserve,
	}
}

// Start binds both lanes to handle and begins draining. It returns once the
// first consumer is running; the pool and autoscaler stop when ctx is
// cancelled.
func (c *Consumer) Start(ctx context.Context, handle Handler) error {
	return bus.ConsumeWeighted(ctx, c.nrApp, []bus.WeightedLane{
		{Sub: c.sub, Subject: c.lanes.PremiumSubject, Handle: handle, Reserve: c.premiumReserve},
		{Sub: c.sub, Subject: c.lanes.StandardSubject, Handle: handle},
	}, c.policy, c.log)
}
