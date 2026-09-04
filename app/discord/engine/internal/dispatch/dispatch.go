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

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

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

func (d *Dispatcher) publishAll(ctx context.Context, cmds []ddiscord.Command) {
	for _, c := range cmds {
		if err := d.Publish(ctx, c); err != nil {
			d.Log.Warn("discord command publish failed", zap.String("type", c.Type), zap.Error(err))
		}
	}
}
