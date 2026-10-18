// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"slices"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func linkMessage() map[string]any {
	return map[string]any{
		"id": "m1", "guild_id": testGuild, "channel_id": "chat", "content": "join discord.gg/abc",
		"author": map[string]any{"id": "u1", "username": "Ada"},
	}
}

func TestADisabledModuleDoesNotRunWhileItsSiblingsDo(t *testing.T) {
	h := newHarness(t, ddiscord.Config{
		GuildID: testGuild, WelcomeChannelID: testWelcomeCh, MemberRoleID: testMemberRole, AutoRoleEnabled: "off",
	})

	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))

	require.Len(t, h.log.byType(ddiscord.TypePostEmbed), 1, "welcome still posts")
	require.Empty(t, h.log.byType(ddiscord.TypeAddRole), "the disabled autorole module stays silent")
}

func TestAnAbsentOptInModuleDoesNotRun(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})

	h.send("MESSAGE_CREATE", linkMessage())

	require.Zero(t, h.guard.seen)
}

func TestAnOptedInModuleRuns(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, LinkGuardEnabled: "on"})

	h.send("MESSAGE_CREATE", linkMessage())

	require.Equal(t, 1, h.guard.seen)
}

func TestAnAbsentDefaultModuleRuns(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})
	h.store.PutGuildModules(discordstore.Guild{ID: testGuild}, ddiscord.GuildModules{GuildID: testGuild, Rows: map[string]ddiscord.ModuleRow{}})
	daily := interaction{data: map[string]any{"name": "daily"}, member: map[string]any{"user": map[string]any{"id": "u1"}}}

	h.interact(daily)

	followups := h.log.followups(t)
	require.Len(t, followups, 1)
	require.NotEmpty(t, followups[0].Embed.Description, "levels answers with no row stored")
}

func TestAnInteractionForADisabledModuleIsAnsweredNotDropped(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, LevelsEnabled: "off"})
	daily := interaction{data: map[string]any{"name": "daily"}, member: map[string]any{"user": map[string]any{"id": "u1"}}}

	h.interact(daily)

	followups := h.log.followups(t)
	require.Len(t, followups, 1)
	require.Equal(t, "That feature is turned off.", followups[0].Content)
}

func TestTheDeskIsNotPostedWhileTicketsAreOff(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, TicketChannelID: testSupportCh, TicketsEnabled: "off", WelcomeEnabled: "off"})

	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))

	require.Empty(t, h.log.byType(ddiscord.TypePostPanel))
}

func TestAHandlerSeesOnlyItsOwnModuleSettings(t *testing.T) {
	seen := map[string][]string{}
	probe := func(name string) module.Module {
		return module.NewModule(name).On("GUILD_MEMBER_ADD", func(_ context.Context, c *module.Context, _ module.Emit) error {
			var fields map[string]any
			require.NoError(t, codec.Unmarshal(c.Settings, &fields))
			for key := range fields {
				seen[name] = append(seen[name], key)
			}
			return nil
		}).Build()
	}
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh, VoiceHubID: testVoiceHub, LogChannelID: testLogsCh})
	h.d.Registry = registry.New(
		probe(ddiscord.ModuleWelcome), probe(ddiscord.ModuleVoice), probe(ddiscord.ModuleLogs),
		module.NewModule(ddiscord.ModuleLevels).On("GUILD_MEMBER_ADD", func(_ context.Context, _ *module.Context, _ module.Emit) error {
			seen["levels-ran"] = append(seen["levels-ran"], "yes")
			return nil
		}).Build(),
	)

	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))

	for _, name := range []string{ddiscord.ModuleWelcome, ddiscord.ModuleVoice, ddiscord.ModuleLogs} {
		spec, ok := ddiscord.Spec(name)
		require.True(t, ok)
		require.NotEmpty(t, seen[name], name)
		for _, key := range seen[name] {
			require.True(t, slices.Contains(spec.Fields, key), "%s handler saw foreign field %q", name, key)
		}
	}
	require.Equal(t, []string{"yes"}, seen["levels-ran"], "a module with no settings still runs")
}
