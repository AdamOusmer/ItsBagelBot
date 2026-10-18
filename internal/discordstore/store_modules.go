// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"

	ddiscord "ItsBagelBot/internal/domain/discord"
	pkg_valkey "ItsBagelBot/pkg/valkey"
)

func (m *Mem) GuildModules(_ context.Context, g Guild) (ddiscord.GuildModules, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if got, ok := m.modules[g.ID]; ok {
		return got, true
	}
	legacy, ok := m.configs[g.ID]
	if !ok {
		return ddiscord.GuildModules{}, false
	}
	return splitLegacy(g, legacy), true
}

func splitLegacy(g Guild, legacy memConfig) ddiscord.GuildModules {
	rows := make(map[string]ddiscord.ModuleRow)
	for _, row := range ddiscord.SplitConfig(legacy.Config) {
		row.Version = legacy.Version
		rows[row.Name] = row
	}
	return ddiscord.GuildModules{GuildID: g.ID, Rows: rows}
}

func (m *Mem) modulesLocked(g Guild) ddiscord.GuildModules {
	if got, ok := m.modules[g.ID]; ok {
		return got
	}
	if legacy, ok := m.configs[g.ID]; ok {
		return splitLegacy(g, legacy)
	}
	return ddiscord.GuildModules{GuildID: g.ID}
}

func (m *Mem) SetGuildModule(_ context.Context, set SetModule) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.guild[set.Guild.ID] != set.Broadcaster.ID || set.Broadcaster.ID == "" {
		return 0, ErrNotBound
	}
	current := m.modulesLocked(set.Guild)
	if current.Rows[set.Row.Name].Version != set.ExpectedVersion {
		return 0, ErrConfigConflict
	}
	set.Row.Version = set.ExpectedVersion + 1
	rows := make(map[string]ddiscord.ModuleRow, len(current.Rows)+1)
	for name, row := range current.Rows {
		rows[name] = row
	}
	rows[set.Row.Name] = set.Row
	m.modules[set.Guild.ID] = ddiscord.GuildModules{GuildID: set.Guild.ID, Rows: rows}
	return set.Row.Version, nil
}

func (m *Mem) PutGuildModules(g Guild, mods ddiscord.GuildModules) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mods.GuildID = g.ID
	m.modules[g.ID] = mods
}

func (m *Mem) cachedModules(ctx context.Context, g Guild) (ddiscord.GuildModules, bool) {
	return m.GuildModules(ctx, g)
}

func (m *Mem) cacheModules(_ context.Context, g Guild, mods ddiscord.GuildModules) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modules[g.ID] = mods
}

func (m *Mem) dropModules(_ context.Context, g Guild) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.modules, g.ID)
}

func (s valkeyStore) cachedModules(ctx context.Context, g Guild) (ddiscord.GuildModules, bool) {
	return pkg_valkey.GetJSON[ddiscord.GuildModules](ctx, s.kv(), modulesKey(g))
}

func (s valkeyStore) cacheModules(ctx context.Context, g Guild, mods ddiscord.GuildModules) {
	if g.ID == "" {
		return
	}
	at := pkg_valkey.Key{Name: modulesKey(g), TTL: configCacheTTL}
	_ = pkg_valkey.SetJSON(ctx, s.kv(), at, mods)
}

func (s valkeyStore) dropModules(ctx context.Context, g Guild) {
	_ = s.kv().Del(ctx, modulesKey(g))
}
