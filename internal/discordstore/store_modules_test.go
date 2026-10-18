// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"context"
	"testing"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
)

func memModuleScenarios() []scenario {
	live := ddiscord.ModuleRow{Name: ddiscord.ModuleLive, Enabled: true, Settings: []byte(`{"liveChannelId":"1"}`)}
	stored := live
	stored.Version = 1
	storedMods := ddiscord.GuildModules{GuildID: "g1", Rows: map[string]ddiscord.ModuleRow{live.Name: stored}}
	write := func(expected int) op {
		return callResult(store.SetGuildModule, discordstore.SetModule{Guild: g1, Broadcaster: owner, Row: live, ExpectedVersion: expected})
	}
	return []scenario{{
		name: "a module write versions its own row and refuses a stale or unbound writer",
		steps: []step{
			{do: write(0), want: outcome(0, discordstore.ErrNotBound)},
			{do: call(store.BindGuild, bound), want: nil},
			{do: write(0), want: outcome(1, nil)},
			{do: write(0), want: outcome(0, discordstore.ErrConfigConflict)},
			{do: callFound(store.GuildModules, g1), want: pair(storedMods, true)},
			{do: write(1), want: outcome(2, nil)},
		},
	}, {
		name:  "no rows and no legacy config reads not found",
		steps: []step{{do: callFound(store.GuildModules, g1), want: noModules}},
	}}
}

func TestMemServesALegacyConfigAsSplitModules(t *testing.T) {
	mem := discordstore.NewMem()
	mem.PutGuildConfig(g1, ddiscord.Config{LiveChannelID: "1", LiveEnabled: "off", LinkGuardEnabled: "on"})

	mods, ok := mem.GuildModules(context.Background(), g1)

	require.True(t, ok)
	require.Equal(t, "g1", mods.GuildID)
	require.False(t, mods.On(ddiscord.ModuleLive))
	require.True(t, mods.On(ddiscord.ModuleLinkGuard))
	require.Equal(t, 1, mods.Rows[ddiscord.ModuleLive].Version)

	mem.PutGuildModules(g1, ddiscord.GuildModules{Rows: map[string]ddiscord.ModuleRow{}})
	mods, _ = mem.GuildModules(context.Background(), g1)
	require.True(t, mods.On(ddiscord.ModuleLive), "stored modules win over the legacy config")
}
