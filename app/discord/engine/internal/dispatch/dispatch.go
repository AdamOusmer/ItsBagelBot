// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package dispatch is the single point every discord.ingress.event.*
// message flows through: decode, resolve the guild's config, run whichever
// module.Handler(s) it maps to, publish whatever they emit. It plays the
// role app/twitch/sesame/engine's Pipeline plays for chat, sized down to what
// Discord's dispatch actually needs (see module's package doc for why there
// is no command/role/cooldown gating stage here).
package dispatch

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// Dispatcher wires the registry to the resolver and the outgress publisher.
type Dispatcher struct {
	Registry *registry.Registry
	Resolver resolve.Resolver
	Store    discordstore.Store
	Publish  modules.Publish
	Log      *zap.Logger
}

// Handle decodes one discord.ingress.event.* message, resolves its guild's
// config, and runs the module(s) it maps to. Always returns nil (ack): a
// malformed event or an unbound guild both have nothing a redelivery would
// fix differently, matching community's own silent "not bound, do nothing"
// convention before this split. A Handler's own error is logged and
// swallowed for the same reason -- one misbehaving module must not nack a
// message every other module already finished handling.
func (d *Dispatcher) Handle(msg *bus.Message) error {
	var ev ddiscord.Event
	if err := codec.Unmarshal(msg.Payload, &ev); err != nil {
		d.Log.Warn("dropping malformed discord ingress event", zap.Error(err))
		return nil
	}
	ctx := msg.Context()
	cfg, broadcasterID, ok := d.Resolver.ByGuild(ctx, ev.GuildID)
	if !ok {
		return nil
	}

	var emitted []ddiscord.Command
	emit := func(c ddiscord.Command) { emitted = append(emitted, c) }

	c := &module.Context{Event: ev, Config: cfg, BroadcasterID: broadcasterID, Log: d.Log}
	modules.EnsureDesk(ctx, d.Store, cfg, emit)
	d.runHandlers(ctx, c, emit)

	d.publishAll(ctx, emitted)
	return nil
}

// runHandlers takes the already-built module.Context rather than its own
// (ev, cfg, broadcasterID) triple -- Handle assembles that Context once and
// this just runs it (CodeScene: Excess Number of Function Arguments).
func (d *Dispatcher) runHandlers(ctx context.Context, c *module.Context, emit module.Emit) {
	for _, h := range d.handlersFor(c.Event) {
		if err := h(ctx, c, emit); err != nil {
			d.Log.Warn("discord module handler failed", zap.String("event_type", c.Event.Type), zap.Error(err))
		}
	}
}

// handlersFor resolves which Handler(s) an event maps to: an
// INTERACTION_CREATE dispatches on its slash-command name or button custom
// id (exactly one owner each, see registry.Registry), every other type
// dispatches on the raw gateway type (any number of interested modules).
func (d *Dispatcher) handlersFor(ev ddiscord.Event) []module.Handler {
	if ev.Type != "INTERACTION_CREATE" {
		return d.Registry.Events(ev.Type)
	}
	in, err := decode.Decode[decode.InteractionEvent](ev.Raw)
	if err != nil {
		// Silently dropping this was how a whole guild's buttons could stop
		// working with nothing anywhere saying so: an interaction that fails
		// to decode has already been deferred by ingress, so the user sees
		// "thinking..." forever and no handler ever ran.
		d.Log.Warn("dropping undecodable discord interaction",
			zap.String("event_type", ev.Type), zap.String("guild_id", ev.GuildID), zap.Error(err))
		return nil
	}
	if in.Data.CustomID != "" {
		if h, ok := d.Registry.Button(in.Data.CustomID); ok {
			return []module.Handler{h}
		}
		return nil
	}
	if h, ok := d.Registry.Slash(in.Data.Name); ok {
		return []module.Handler{h}
	}
	return nil
}

// Publish retry budget. A command that fails to publish is gone: the
// ingress message is ACKed either way (see Handle), so nothing redelivers
// it and the user's button press simply does nothing. Three attempts over
// 200 ms clears the one failure preAdmission can prove is safe to repeat --
// a lane whose stream is momentarily unprovisioned, which answers no
// responders -- without holding the ingress consumer long enough to matter
// (its AckWait is measured in seconds, not milliseconds).
const (
	publishAttempts   = 3
	publishRetryDelay = 100 * time.Millisecond
)

func (d *Dispatcher) publishAll(ctx context.Context, cmds []ddiscord.Command) {
	for _, c := range cmds {
		if err := d.publishRetry(ctx, c); err != nil {
			// ERROR, not WARN: this is a command the user asked for that
			// either nothing will ever run, or ran without us knowing. The
			// subject is logged next to the type because that pair is what
			// an operator needs to go look for the message on the stream and
			// settle which of the two it was.
			d.Log.Error("discord command publish failed",
				zap.String("subject", ddiscord.Lane(c.Type)),
				zap.String("type", c.Type),
				zap.Bool("retried", preAdmission(err)),
				zap.Error(err))
		}
	}
}

// preAdmission reports whether err proves the broker stored nothing, which
// is the only condition under which republishing is safe.
//
// pkg/bus offers no idempotent republish to lean on instead. Publication.ID
// looks like one, but Publisher.PublishOwnedWithID's own contract says the
// ID "is not sent as Nats-Msg-Id and does not make replays idempotent", and
// the Publisher doc states outright that fleet publishing does not use
// broker deduplication -- so a deterministic id would buy nothing here, and
// wiring one would only make the duplicate look sanctioned.
//
// This classification is only meaningful because Publish is wired to the
// CONFIRMED path (main.confirmedPublisher -> bus.PublishConfirmed). The
// asynchronous path returns before the broker answers: its only errors are
// "bus: publisher is closed" and the caller's context error, and the real
// cohort verdict is stashed for a Flush nobody here calls
// (pkg/bus/batch_publisher.go, admit + complete + takeWindowErrLocked).
//
// nats.ErrConnectionClosed used to be treated as proof and is deliberately
// gone. joinAsyncCohort wraps a refused send with %w AFTER everything
// nats.go already accepted is on the wire and normally stored: "bus: async
// cohort sent and stored 5/8 messages; the remainder never reached the
// wire: %w". errors.Is cannot tell that from a cohort that stored nothing,
// and joinAsyncCohort's own doc says the consequence out loud -- "A caller
// that retries on this error stores the sent prefix twice". A closed
// connection is also the one error a 200 ms retry could not fix anyway.
//
// No responders is what survives. JetStream answers it when no stream is
// listening on the subject at all, one batch worker's whole cohort belongs
// to a single stream (publisherPool.streamFor picks the worker), so a
// no-responders verdict is proof the broker stored none of it. A PubAck
// timeout is NOT -- JetStream may have stored the message and lost only the
// acknowledgement, exactly the ambiguous outcome pkg/bus documents as
// dropped rather than replayed. Republishing there posts the streamer's
// message into their guild twice, which is worse than the miss the retry
// was trying to avoid, so anything unrecognised falls to the safe side and
// is logged instead.
func preAdmission(err error) bool {
	return errors.Is(err, nats.ErrNoResponders)
}

func (d *Dispatcher) publishRetry(ctx context.Context, c ddiscord.Command) error {
	var err error
	for attempt := range publishAttempts {
		if err = d.Publish(ctx, c); err == nil {
			return nil
		}
		if !preAdmission(err) || attempt == publishAttempts-1 {
			break
		}
		if waitErr := sleep(ctx, publishRetryDelay); waitErr != nil {
			// Shutting down: report the publish failure, not the
			// cancellation, so the log names what was actually lost.
			return err
		}
	}
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
