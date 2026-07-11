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
package consumer

import (
	"context"

	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Handler is the per-message routine the consumer hands each event to. The engine
// pipeline's Process satisfies it.
type Handler func(*message.Message) error

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
	sub   message.Subscriber
	nrApp *newrelic.Application
	log   *zap.Logger
	cfg   Config
}

// New builds a Consumer over one subscriber.
func New(sub message.Subscriber, nrApp *newrelic.Application, cfg Config, log *zap.Logger) *Consumer {
	return &Consumer{sub: sub, nrApp: nrApp, log: log, cfg: cfg}
}

// Start binds both lanes to handle and begins draining. It returns once the first
// consumer is running; the pool and autoscaler stop when ctx is cancelled. The
// returned *bus.Weighted lets the caller drain in-flight handlers on shutdown.
func (c *Consumer) Start(ctx context.Context, handle Handler) (*bus.Weighted, error) {
	return bus.ConsumeWeighted(ctx, c.nrApp, []bus.WeightedLane{
		{Sub: c.sub, Subject: c.cfg.Lanes.PremiumSubject, Handle: handle, Reserve: c.cfg.PremiumReserve},
		{Sub: c.sub, Subject: c.cfg.Lanes.StandardSubject, Handle: handle},
	}, c.cfg.Policy, c.log)
}
