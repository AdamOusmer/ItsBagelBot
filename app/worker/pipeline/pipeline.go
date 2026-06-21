// Package pipeline is the worker's processing stage. It is not the consumer:
// the weighted consumer (bus.ConsumeWeighted) owns the subscription and the
// goroutine pool, pulls up to N messages in flight, and hands each one to
// Pipeline.Process. The pipeline is purely the per-message work — decode,
// route by type, act, publish — and is the single starting point every message
// flows through once the consumer has handed it off.
//
// Ack discipline: Process returns nil only after the resulting request has
// been published to outgress (the JetStream publish is synchronous, so a nil
// return means the broker has the message). Any failure before that returns an
// error, which the consumer turns into a nack so the event is redelivered. The
// worker therefore never acks an event whose action has not been sent.
package pipeline

import (
	"context"
	"encoding/json"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"go.uber.org/zap"
)

// handler runs one event type. It returns the outgress message to publish, or
// nil when the event produces no outbound action.
type handler func(ctx context.Context, env Envelope, regress Regress) (*outgress.Message, error)

// Pipeline holds the dependencies shared by every stage.
type Pipeline struct {
	log  *zap.Logger
	pub  message.Publisher
	proj projection.Reader

	outgressPremium  string
	outgressStandard string

	routes map[string]handler
}

// NewPipeline constructs a Pipeline and registers the per-type routes.
func NewPipeline(
	log *zap.Logger,
	pub message.Publisher,
	proj projection.Reader,
	outgressPremium, outgressStandard string,
) *Pipeline {
	p := &Pipeline{
		log:              log,
		pub:              pub,
		proj:             proj,
		outgressPremium:  outgressPremium,
		outgressStandard: outgressStandard,
	}

	// One route per EventSub type. Unmapped types are acked and ignored, so
	// adding a feature is just adding a stage here.
	p.routes = map[string]handler{
		"channel.chat.message":         p.handleChatMessage,
		"stream.online":                p.handleStream,
		"stream.offline":               p.handleStream,
		"channel.follow":               p.handleFollow,
		"channel.subscribe":            p.handleSubscribe,
		"channel.subscription.message": p.handleSubscribe,
		"channel.subscription.gift":    p.handleSubscribe,
		"channel.cheer":                p.handleCheer,
		"channel.raid":                 p.handleRaid,
	}

	return p
}

// Process is the handler the weighted consumer hands each message to. It
// decodes the envelope, recovers the regress status, dispatches to the type's
// stage, and publishes whatever the stage produced. It returns nil (ack) only
// once that publish has gone through, and an error (nack) on any failure.
func (p *Pipeline) Process(msg *message.Message) error {
	ctx := msg.Context()

	var env Envelope
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		// A malformed envelope is poison: redelivering it forever helps no
		// one, so drop it (ack) and move on.
		p.log.Warn("dropping malformed envelope",
			zap.String("message_id", msg.UUID),
			zap.Error(err),
		)
		return nil
	}

	regress := regressFromLane(env.Lane)

	route, ok := p.routes[env.Type]
	if !ok {
		p.log.Debug("no route for event type, acking",
			zap.String("type", env.Type),
			zap.String("regress", regress.String()),
		)
		return nil
	}

	out, err := route(ctx, env, regress)
	if err != nil {
		// Real processing failure: nack so JetStream redelivers. Handlers
		// must stay idempotent.
		return err
	}
	if out == nil {
		// The event legitimately produced no outbound request (no matching
		// command, module disabled, ...). There is nothing to send, so ack.
		return nil
	}

	// Ack only after the request is on the outgress lane: a publish failure
	// returns the error so the consumer nacks and the event is redelivered.
	return p.emit(ctx, regress, out)
}

// emit publishes an outgress message onto the lane matching the regress
// status, so a premium broadcaster's reply rides the premium lane end to end.
// Stream-lane traffic that produces an action is treated as standard. The
// JetStream publish is synchronous, so a nil return means outgress has it.
func (p *Pipeline) emit(ctx context.Context, regress Regress, out *outgress.Message) error {
	subject := p.outgressStandard
	if regress == RegressPremium {
		subject = p.outgressPremium
	}
	return bus.PublishJSON(ctx, p.pub, subject, out)
}
