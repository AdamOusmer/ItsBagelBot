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

const maxWarned = 4096

type ConfigWarnings struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewConfigWarnings() *ConfigWarnings { return &ConfigWarnings{seen: map[string]struct{}{}} }

func (w *ConfigWarnings) first(guildID, field string) bool {
	if w == nil {
		return true
	}
	key := guildID + "\x00" + field
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, seen := w.seen[key]; seen {
		return false
	}
	if len(w.seen) >= maxWarned {
		clear(w.seen)
	}
	w.seen[key] = struct{}{}
	return true
}

type GuildModulesOf struct {
	Guild   discordstore.Guild
	Modules ddiscord.GuildModules
}

func (r Resolver) ByBroadcaster(ctx context.Context, broadcasterID uint64) []GuildModulesOf {
	if r.Store == nil || !r.gateOpen(ctx, broadcasterID) {
		return nil
	}
	guilds, err := r.Store.GuildsOf(ctx, discordstore.Broadcaster{ID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		r.log().Error("discord guild list failed; this Twitch event fans out to nothing",
			zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return nil
	}
	out := make([]GuildModulesOf, 0, len(guilds))
	for _, bound := range guilds {
		mods, ok := r.modulesOf(ctx, bound.Guild)
		if !ok {
			continue
		}
		out = append(out, GuildModulesOf{Guild: bound.Guild, Modules: mods})
	}
	return out
}

func (r Resolver) ByGuild(ctx context.Context, guildID string) (ddiscord.GuildModules, string, bool) {
	if r.Store == nil {
		return ddiscord.GuildModules{}, "", false
	}
	b, ok := r.Store.Broadcaster(ctx, discordstore.Guild{ID: guildID})
	if !ok {
		r.dropped(guildID, dropUnbound, 0)
		return ddiscord.GuildModules{}, "", false
	}
	id, err := strconv.ParseUint(b.ID, 10, 64)
	if err != nil {
		r.dropped(guildID, dropBadBroadcaster, 0)
		return ddiscord.GuildModules{}, "", false
	}
	if reason := r.gateClosed(ctx, id); reason != "" {
		r.dropped(guildID, reason, id)
		return ddiscord.GuildModules{}, "", false
	}
	mods, ok := r.modulesOf(ctx, discordstore.Guild{ID: guildID})
	if !ok {
		r.dropped(guildID, dropNoConfig, id)
		return ddiscord.GuildModules{}, "", false
	}
	return mods, b.ID, true
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
	if !r.shouldWarn(guildID, reason) {
		return
	}
	r.log().Warn("dropping discord events for guild",
		zap.String("guild_id", guildID), zap.Uint64("broadcaster_id", broadcasterID), zap.String("reason", reason))
}

func (r Resolver) shouldWarn(guildID, reason string) bool {
	return guildID != "" && reason != dropLoggedElsewhere && r.Warned.first(guildID, reason)
}

func (r Resolver) sanitizeRow(guildID string, row ddiscord.ModuleRow) ddiscord.ModuleRow {
	clean, bad := ddiscord.SanitizeModule(row.Name, row.Settings)
	for _, fe := range bad {
		if !r.Warned.first(guildID, fe.Field) {
			continue
		}
		r.log().Warn("discord config field is invalid; ignoring it",
			zap.String("guild_id", guildID),
			zap.String("field", fe.Field), zap.String("code", fe.Code))
	}
	row.Settings = clean
	return row
}

func (r Resolver) modulesOf(ctx context.Context, g discordstore.Guild) (ddiscord.GuildModules, bool) {
	stored, ok := r.Store.GuildModules(ctx, g)
	if !ok {
		return ddiscord.GuildModules{}, false
	}
	rows := make(map[string]ddiscord.ModuleRow, len(stored.Rows))
	for name, row := range stored.Rows {
		if _, known := ddiscord.Spec(name); known {
			rows[name] = r.sanitizeRow(g.ID, row)
		}
	}
	return ddiscord.GuildModules{GuildID: g.ID, Rows: rows}, true
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
