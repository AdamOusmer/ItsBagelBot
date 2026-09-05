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
	"sync"

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

// Status returns a broadcaster's projected account status ("free", "paid",
// "vip") and whether it could be read at all.
type Status func(ctx context.Context, broadcasterID uint64) (string, bool)

// Resolver ties a guild-binding store to the module-blob reader.
type Resolver struct {
	Store   discordstore.Store
	Modules Modules
	// Tier gates the beta: Discord is premium-only while
	// ddiscord.BetaPremiumOnly holds. It sits HERE, on the one lookup every
	// module and both input families already go through, rather than in each
	// module. A per-module check is a check a new module can forget, and the
	// failure mode of forgetting is handing a paid beta to everyone.
	//
	// Nil while the gate is on resolves nothing, so a service that forgets to
	// wire it fails closed and loudly rather than serving every channel.
	Tier Status
	// Warned dedupes the invalid-config warning. It is a POINTER so the
	// copies of this value that dispatch and every module hold share one
	// record; nil (tests, and any caller that forgets it) simply warns
	// every time, which is noisy rather than wrong.
	Warned *ConfigWarnings
	Log    *zap.Logger
}

// ConfigWarnings remembers which (guild, field) pairs have already been
// warned about.
//
// Without it the warning fires on EVERY event for a guild whose stored
// config holds one bad field -- thousands of identical lines an hour, which
// is how a real signal gets filtered out of the log pipeline and then
// ignored. Deduping is per field rather than per guild because a second bad
// field is new information.
type ConfigWarnings struct{ seen sync.Map }

// NewConfigWarnings builds an empty record. Its lifetime is the process:
// the set is bounded by (guilds x Config fields) and each entry is two
// short strings, so nothing here needs eviction.
func NewConfigWarnings() *ConfigWarnings { return &ConfigWarnings{} }

// first reports whether this pair has not been warned about yet.
func (w *ConfigWarnings) first(guildID, field string) bool {
	if w == nil {
		return true
	}
	_, seen := w.seen.LoadOrStore(guildID+"\x00"+field, struct{}{})
	return !seen
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
	cfg := r.sanitize(ddiscord.Parse(mod.Configs))
	if !cfg.Connected() {
		return ddiscord.Config{}, false
	}
	if !r.premiumOK(ctx, broadcasterID) {
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

// sanitize drops the fields the stored blob got wrong and warns once per
// guild per field. It runs on every resolve rather than at write time
// because the blob is also written by older console builds and by hand; the
// validation pass it costs is a map build and a sort, well under the
// projection read it follows.
func (r Resolver) sanitize(cfg ddiscord.Config) ddiscord.Config {
	clean, bad := ddiscord.SanitizeConfig(cfg)
	for _, fe := range bad {
		if !r.Warned.first(cfg.GuildID, fe.Field) {
			continue
		}
		r.log().Warn("discord config field is invalid; ignoring it",
			zap.String("guild_id", cfg.GuildID),
			zap.String("field", fe.Field), zap.String("code", fe.Code))
	}
	return clean
}

// premiumOK applies the beta gate. It runs LAST, after the row is known to
// exist and be connected, so the common free-channel case (no Discord row at
// all) never pays a tier lookup.
func (r Resolver) premiumOK(ctx context.Context, broadcasterID uint64) bool {
	if !ddiscord.BetaPremiumOnly {
		return true
	}
	if r.Tier == nil {
		r.log().Warn("discord beta gate has no tier reader; refusing to serve",
			zap.Uint64("broadcaster_id", broadcasterID))
		return false
	}
	status, known := r.Tier(ctx, broadcasterID)
	if open := ddiscord.PremiumGateOpen(status, known); !open {
		r.log().Debug("discord is premium-only in beta; skipping channel",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("status", status))
		return false
	}
	return true
}

func (r Resolver) log() *zap.Logger {
	if r.Log != nil {
		return r.Log
	}
	return zap.NewNop()
}
