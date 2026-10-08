// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

type Modules interface {
	GetModule(ctx context.Context, userID uint64, name string) (projection.ModuleView, bool, error)
}

type Status func(ctx context.Context, broadcasterID uint64) (string, bool)

type Resolver struct {
	Store   discordstore.Store
	Modules Modules
	Tier    Status
	Warned  *ConfigWarnings
	Log     *zap.Logger
}

type ConfigWarnings struct{ seen sync.Map }

func NewConfigWarnings() *ConfigWarnings { return &ConfigWarnings{} }

func (w *ConfigWarnings) first(guildID, field string) bool {
	if w == nil {
		return true
	}
	_, seen := w.seen.LoadOrStore(guildID+"\x00"+field, struct{}{})
	return !seen
}

func (r Resolver) ByBroadcaster(ctx context.Context, broadcasterID uint64) []discordstore.GuildConfigOf {
	if r.Store == nil || !r.gateOpen(ctx, broadcasterID) {
		return nil
	}
	guilds, err := r.Store.GuildsOf(ctx, discordstore.Broadcaster{ID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		r.log().Error("discord guild list failed; this Twitch event fans out to nothing",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return nil
	}
	out := make([]discordstore.GuildConfigOf, 0, len(guilds))
	for _, bound := range guilds {
		cfg, ok := r.configOf(ctx, bound.Guild)
		if !ok {
			continue
		}
		out = append(out, discordstore.GuildConfigOf{Guild: bound.Guild, Config: cfg})
	}
	return out
}

func (r Resolver) ByGuild(ctx context.Context, guildID string) (ddiscord.Config, string, bool) {
	if r.Store == nil {
		return ddiscord.Config{}, "", false
	}
	b, ok := r.Store.Broadcaster(ctx, discordstore.Guild{ID: guildID})
	if !ok {
		r.dropped(guildID, dropUnbound, 0)
		return ddiscord.Config{}, "", false
	}
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		r.dropped(guildID, dropBadBroadcaster, 0)
		return ddiscord.Config{}, "", false
	}
	if reason := r.gateClosed(ctx, id); reason != "" {
		r.dropped(guildID, reason, id)
		return ddiscord.Config{}, "", false
	}
	cfg, ok := r.configOf(ctx, discordstore.Guild{ID: guildID})
	if !ok {
		r.dropped(guildID, dropNoConfig, id)
		return ddiscord.Config{}, "", false
	}
	return cfg, b.ID, true
}

const (
	dropUnbound         = "guild is not bound to a broadcaster"
	dropBadBroadcaster  = "guild binding has an invalid broadcaster id"
	dropModuleOff       = "discord module is off for the broadcaster"
	dropPremiumClosed   = "discord is premium-only in beta and the broadcaster is not premium"
	dropNoConfig        = "guild has no settings row"
	dropLoggedElsewhere = "gate refused and already logged"
)

func (r Resolver) dropped(guildID, reason string, broadcasterID uint64) {
	if guildID == "" || reason == dropLoggedElsewhere || !r.Warned.first(guildID, reason) {
		return
	}
	r.log().Warn("dropping discord events for guild",
		zap.String("guild_id", guildID), zap.Uint64("broadcaster_id", broadcasterID), zap.String("reason", reason))
}

func (r Resolver) sanitize(guildID string, cfg ddiscord.Config) ddiscord.Config {
	clean, bad := ddiscord.SanitizeConfig(cfg)
	for _, fe := range bad {
		if !r.Warned.first(guildID, fe.Field) {
			continue
		}
		r.log().Warn("discord config field is invalid; ignoring it",
			zap.String("guild_id", guildID),
			zap.String("field", fe.Field), zap.String("code", fe.Code))
	}
	return clean
}

func (r Resolver) configOf(ctx context.Context, g discordstore.Guild) (ddiscord.Config, bool) {
	cfg, _, ok := r.Store.GuildConfig(ctx, g)
	if !ok {
		return ddiscord.Config{}, false
	}
	cfg = r.sanitize(g.ID, cfg)
	cfg.GuildID = g.ID
	return cfg, true
}

func (r Resolver) gateOpen(ctx context.Context, broadcasterID uint64) bool {
	return r.gateClosed(ctx, broadcasterID) == ""
}

func (r Resolver) gateClosed(ctx context.Context, broadcasterID uint64) string {
	if r.Modules == nil {
		return dropLoggedElsewhere
	}
	mod, found, err := r.Modules.GetModule(ctx, broadcasterID, ddiscord.ModuleName)
	if err != nil {
		r.log().Warn("discord module read failed; treating as not connected",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return dropLoggedElsewhere
	}
	if !found || !mod.IsEnabled {
		return dropModuleOff
	}
	return r.premiumClosed(ctx, broadcasterID)
}

func (r Resolver) premiumClosed(ctx context.Context, broadcasterID uint64) string {
	if !ddiscord.BetaPremiumOnly {
		return ""
	}
	if r.Tier == nil {
		r.log().Warn("discord beta gate has no tier reader; refusing to serve",
			zap.Uint64("broadcaster_id", broadcasterID))
		return dropLoggedElsewhere
	}
	status, known := r.Tier(ctx, broadcasterID)
	if open := ddiscord.PremiumGateOpen(status, known); !open {
		r.log().Debug("discord is premium-only in beta; skipping channel",
			zap.Uint64("broadcaster_id", broadcasterID), zap.String("status", status))
		return dropPremiumClosed
	}
	return ""
}

func (r Resolver) log() *zap.Logger {
	if r.Log != nil {
		return r.Log
	}
	return zap.NewNop()
}
