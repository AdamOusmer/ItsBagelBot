// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package module is app/discord/engine's module authoring surface, built to
// the same shape as app/sesame/module (a fluent Builder producing an
// immutable Module the engine indexes) for the reason the task that created
// this package gave: a reader who already knows sesame's shape should not
// have to learn a second one for Discord.
//
// The shape is the same; the content is not, because Discord's dispatch unit
// is not sesame's. Sesame indexes by chat COMMAND (a "!trigger" parsed out of
// free text, gated by role/cooldown/live-only) with EventSub types as a
// secondary axis. Discord's own client already structures a slash command and
// a button press for the bot -- there is no text to parse, no role ladder to
// climb (Discord permissions are a bitmask read straight off the interaction),
// and no cooldown or live-only gate any of the ported behavior used. Carrying
// sesame's Command/CmdBuilder/Role/Cooldown machinery over anyway would be
// dead weight bolted on to satisfy a shape rather than a need, so this
// package keeps exactly three dispatch axes: a raw gateway event Type, a
// slash-command name, and a button's custom id -- the three sesame's own
// community package (app/dingress/internal/community/bot.go's
// communityEvents/slashCmds/buttonCmds maps) already dispatched on before
// this split, just expressed as the fluent Builder the task asked for.
package module

import (
	"context"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

// Context is the per-event state the engine builds and hands to a Handler.
// It is confined to a single consumer goroutine, matching sesame's Context.
type Context struct {
	// Event is the ingress envelope: Type, the routing ids, the raw gateway
	// payload, and the ingress receive timestamp. A Handler decodes the slice
	// of Raw it needs, the same way community's onMemberAdd/onVoice/onMessage
	// did before this split (see internal/decode).
	Event ddiscord.Event
	// Config is the resolved guild's Discord module blob -- the one config
	// every Discord feature reads, unlike sesame's per-named-module blob,
	// because Bagel has exactly one Discord module, not a catalog of them.
	Config ddiscord.Config
	// BroadcasterID is the Twitch user id (decimal string) Config.GuildID is
	// bound to. Set whenever resolution succeeded; a live/clip Handler (whose
	// input is a Twitch event, not a Discord one) is invoked with Event zero
	// and only Config/BroadcasterID set.
	BroadcasterID string
	Log           *zap.Logger
}

// Emit publishes one Command. Unlike sesame's pooled Output/Emit (built for
// a hot chat-message path), Command values here are not pooled: Discord's
// event volume is orders of magnitude below Twitch chat, so the allocation
// discipline that pays for itself on sesame's hot path is not worth the
// complexity here.
type Emit func(cmd ddiscord.Command)

// Handler runs one module's reaction to a gateway event, a slash command, or
// a button press, and emits any Commands. All three dispatch axes share this
// one signature (see the package doc for why there is no separate
// interaction-specific type): a Handler that only ever runs from an
// interaction can assume Context.Event.Type is INTERACTION_CREATE and decode
// accordingly.
type Handler func(ctx context.Context, c *Context, emit Emit) error

// Module is the immutable artifact Build returns.
type Module struct {
	Name    string
	Events  map[string]Handler
	Slash   map[string]Handler
	Buttons map[string]Handler
}
