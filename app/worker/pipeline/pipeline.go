package pipeline

import (
	"ItsBagelBot/pkg/bus"
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// Pipeline holds the dependencies required to process incoming events.
type Pipeline struct {
	log   *zap.Logger
	pub   message.Publisher
	sub   message.Subscriber
	nrApp *newrelic.Application
}

// NewPipeline constructs a new Pipeline instance.
func NewPipeline(log *zap.Logger, pub message.Publisher, sub message.Subscriber, nrApp *newrelic.Application) *Pipeline {
	return &Pipeline{
		log:   log,
		pub:   pub,
		sub:   sub,
		nrApp: nrApp,
	}
}

// Handle processes individual messages received from the subscription.
func (p *Pipeline) Handle(msg *message.Message) error {
	// msg.Context() carries the New Relic transaction wrapper automatically
	ctx := msg.Context()

	p.log.Debug("received ingress event", zap.String("uuid", msg.UUID))

	// TODO: Unmarshal payload and execute pipeline steps
	// 1. Parse payload
	// 2. Find module
	// 3. Check permissions
	// 4. Execute
	// 5. Publish to outgress using p.pub

	p.pub.Publish(bus.OutgressSubject, msg)

	ctx.Done() // TODO: Remove when actual processing is implemented, this is just to illustrate the transaction lifecycle

	// Return nil to Ack, return error to Nack
	return nil
}

// Start wires up the consumer to the specified subject and begins listening.
func (p *Pipeline) Start(ctx context.Context, subject string, concurrency int) error {
	p.log.Info("starting pipeline consumer", zap.String("subject", subject), zap.Int("concurrency", concurrency))

	return bus.ConsumeWeighted(
		ctx,
		p.nrApp,
		p.sub,
		subject,
		p.Handle,
		concurrency,
		p.log,
	)
}
