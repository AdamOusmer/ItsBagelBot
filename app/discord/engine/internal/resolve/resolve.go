// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package resolve looks up the Discord module config every engine module
// needs, in the two directions engine's two input families require: a
// Discord guild id (every discord.ingress.event.* subject) and a Twitch
// broadcaster id (twitch.ingress.event.stream, data.twitch.clip.created).
// Ported from app/dingress/internal/community's Bot.bound (guild direction)
// and app/dingress/internal/egress's Worker.discordConfig (broadcaster
// direction) -- two copies of nearly the same three checks before this
// split, because ROLE=gateway and ROLE=egress each had their own. One
// process (engine) now needs both directions, so they are one package.
package resolve

import (
	"context"
	"strconv"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
)

// Modules reads the Discord module blob for a Twitch broadcaster id. Satisfied
// by *projection.Store in production.
type Modules interface {
	GetModule(ctx context.Context, userID uint64, name string) (projection.ModuleView, bool, error)
}

// Resolver ties a guild-binding store to the module-blob reader.
type Resolver struct {
	Store   discordstore.Store
	Modules Modules
	Log     *zap.Logger
}

// ByBroadcaster loads the enabled, connected Discord config for a Twitch
// broadcaster id. Ported unchanged from egress's Worker.discordConfig: found,
// enabled, and Connected() are checked in the order that lets every call
// site bail on the cheapest check first.
func (r Resolver) ByBroadcaster(ctx context.Context, broadcasterID uint64) (ddiscord.Config, bool) {
	if r.Modules == nil {
		return ddiscord.Config{}, false
	}
	mod, found, err := r.Modules.GetModule(ctx, broadcasterID, ddiscord.ModuleName)
	if err != nil {
		r.log().Warn("discord module read failed; treating as not connected",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return ddiscord.Config{}, false
	}
	if !found || !mod.IsEnabled {
		return ddiscord.Config{}, false
	}
	cfg := ddiscord.Parse(mod.Configs)
	if !cfg.Connected() {
		return ddiscord.Config{}, false
	}
	return cfg, true
}

// ByGuild resolves a Discord guild id to its bound broadcaster's config.
// Ported from community's Bot.bound, minus the ensureDesk side effect (the
// dispatcher runs that explicitly, since it needs to emit a Command -- see
// app/discord/engine/modules/ticket.go's EnsureDesk).
func (r Resolver) ByGuild(ctx context.Context, guildID string) (ddiscord.Config, string, bool) {
	if r.Store == nil {
		return ddiscord.Config{}, "", false
	}
	b, ok := r.Store.Broadcaster(ctx, discordstore.Guild{ID: guildID})
	if !ok {
		return ddiscord.Config{}, "", false
	}
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		return ddiscord.Config{}, "", false
	}
	cfg, ok := r.ByBroadcaster(ctx, id)
	if !ok {
		return ddiscord.Config{}, "", false
	}
	return cfg, b.ID, true
}

func (r Resolver) log() *zap.Logger {
	if r.Log != nil {
		return r.Log
	}
	return zap.NewNop()
}
