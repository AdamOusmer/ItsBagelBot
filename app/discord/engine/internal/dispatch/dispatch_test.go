// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"testing"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMemberEvents(t *testing.T) {
	cases := []struct {
		name       string
		cfg        ddiscord.Config
		event      string
		guildID    string
		wantEmbeds int
		wantRoles  int
	}{
		{"a join welcomes the member and assigns the autorole",
			ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh, MemberRoleID: testMemberRole}, "GUILD_MEMBER_ADD", testGuild, 1, 1},
		{"goodbye is off by default",
			ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh}, "GUILD_MEMBER_REMOVE", testGuild, 0, 0},
		{"a join is logged when the welcome is off",
			ddiscord.Config{GuildID: testGuild, WelcomeEnabled: "off", LogChannelID: testLogsCh}, "GUILD_MEMBER_ADD", testGuild, 1, 0},
		{"an unbound guild is ignored",
			ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh}, "GUILD_MEMBER_ADD", "other", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.cfg)

			h.sendTo(tc.guildID, tc.event, memberPayload(tc.guildID))

			require.Len(t, h.log.byType(ddiscord.TypePostEmbed), tc.wantEmbeds)
			require.Len(t, h.log.byType(ddiscord.TypeAddRole), tc.wantRoles)
		})
	}
}

func hubConfig() ddiscord.Config {
	return ddiscord.Config{GuildID: testGuild, VoiceHubID: testVoiceHub}
}

func (h *harness) joinHub() string {
	h.t.Helper()
	h.send("VOICE_STATE_UPDATE", voicePayload(testVoiceHub))
	return h.channels.created[0]
}

func TestJoiningTheVoiceHubClonesTheRoomAndMovesTheMember(t *testing.T) {
	h := newHarness(t, hubConfig())

	h.joinHub()

	require.Len(t, h.channels.created, 1)
	require.Len(t, h.channels.moved, 1)
	require.Len(t, h.log.byType(ddiscord.TypePostPanel), 1)
}

func TestAnEmptiedVoiceCloneIsDeleted(t *testing.T) {
	h := newHarness(t, hubConfig())
	clone := h.joinHub()

	h.send("VOICE_STATE_UPDATE", voicePayload(clone))
	h.send("VOICE_STATE_UPDATE", voicePayload(""))

	require.Equal(t, []string{clone}, h.channels.deleted)
}

func TestTheVoiceLockButtonLocksTheRoom(t *testing.T) {
	h := newHarness(t, hubConfig())
	clone := h.joinHub()

	h.interact(interaction{channelID: clone, data: map[string]any{"custom_id": discordapi.CustomVoiceLock}, member: memberWith("0")})

	followups := h.log.followups(t)
	require.Len(t, followups, 1)
	require.Equal(t, "Locked.", followups[0].Content)
}

func TestTicketOpenAndClose(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, TicketCategoryID: testTicketCat})

	h.interact(interaction{channelID: testSupportCh, data: map[string]any{"custom_id": discordapi.CustomTicketOpen}, member: memberWith("8")})
	require.Len(t, h.channels.created, 1)
	require.Len(t, h.channels.opened, 1)
	require.Len(t, h.channels.opened[0].Buttons, 2)

	h.interact(interaction{channelID: h.channels.created[0], data: map[string]any{"custom_id": discordapi.CustomTicketClose}, member: memberWith("8")})
	require.Len(t, h.channels.deleted, 1)
}

func TestTicketDeskIsPostedOncePerGuild(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild, TicketChannelID: testSupportCh, WelcomeEnabled: "off"})

	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))
	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))

	require.Len(t, h.log.byType(ddiscord.TypePostPanel), 1)
}

func TestDailyClaimsOncePerDay(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})
	daily := interaction{data: map[string]any{"name": "daily"}, member: map[string]any{"user": map[string]any{"id": "u1"}}}

	h.interact(daily)
	h.interact(daily)

	followups := h.log.followups(t)
	require.Len(t, followups, 2)
	require.Equal(t, "Already claimed today.", followups[1].Embed.Description)
}

func TestKickNeedsModerationPermissions(t *testing.T) {
	kick := map[string]any{"name": "kick", "options": []any{
		map[string]any{"name": "user", "type": 6, "value": "u2"},
	}}
	cases := []struct {
		name        string
		permissions string
		wantKicks   int
	}{
		{"a member without permissions cannot kick", "0", 0},
		{"an admin kick fires", "8", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, ddiscord.Config{GuildID: testGuild})

			h.interact(interaction{data: kick, member: memberWith(tc.permissions)})

			require.Len(t, h.log.byType(ddiscord.TypeKickMember), tc.wantKicks)
		})
	}
}

func TestChatLevelsTheMemberUp(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})
	h.store.SeedXP(discordstore.XPSeed{Member: discordstore.Member{GuildID: testGuild, UserID: "u1"}, Amount: 90})

	h.send("MESSAGE_CREATE", map[string]any{
		"id": "m1", "guild_id": testGuild, "channel_id": "chat", "content": "hi",
		"author": map[string]any{"id": "u1", "username": "Ada"},
	})

	require.Len(t, h.log.byType(ddiscord.TypePostEmbed), 1)
}

func TestUndecodableInteractionIsLogged(t *testing.T) {
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})
	core, logs := observer.New(zapcore.DebugLevel)
	h.d.Log = zap.New(core)

	h.handle(t.Context(), ddiscord.Event{Type: "INTERACTION_CREATE", GuildID: testGuild, Raw: []byte("{not json")})

	require.Empty(t, h.log.cmds, "no module handler may run")
	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	require.Len(t, warns, 1)
	require.Equal(t, map[string]any{"guild_id": testGuild, "event_type": "INTERACTION_CREATE"}, warnFields(warns[0]))
}

func warnFields(entry observer.LoggedEntry) map[string]any {
	fields := entry.ContextMap()
	delete(fields, "error")
	return fields
}
