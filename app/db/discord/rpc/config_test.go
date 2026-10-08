// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/discord/ent"
	"ItsBagelBot/app/db/discord/ent/enttest"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/app/db/discord/rpc"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	guildID       = "123456789012345678"
	broadcasterID = 42
)

type harness struct {
	nc *nats.Conn
}

func newHarness(t *testing.T) harness {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	store := repository.New(client, dialect.SQLite)
	require.NoError(t, store.BindingSet(t.Context(), repository.BindParams{GuildID: guildID, BroadcasterID: broadcasterID}))
	nc := testnats.Connect(t)
	require.NoError(t, rpc.Subscribe(rpc.Wiring{
		RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()},
		Repo:      store,
		Prefix:    "discord",
	}))
	return harness{nc: nc}
}

func call[Reply any](t *testing.T, h harness, verb string, req any) Reply {
	t.Helper()
	payload, err := codec.Marshal(req)
	require.NoError(t, err)
	msg, err := h.nc.Request("discord."+verb, payload, 3*time.Second)
	require.NoError(t, err)
	var reply Reply
	require.NoError(t, codec.Unmarshal(msg.Data, &reply))
	return reply
}

func TestConfigSetValidatesIdsAndTogglesBeforeStoring(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ddiscord.Config
		bad  []string
	}{
		{name: "accepts every control unset", cfg: ddiscord.Config{}},
		{
			name: "accepts well formed snowflakes and toggles",
			cfg: ddiscord.Config{
				GuildID: guildID, LiveChannelID: "12345678901234567", LiveEnabled: "on", VoiceEnabled: "off",
			},
		},
		{name: "accepts a 20 digit snowflake", cfg: ddiscord.Config{LiveChannelID: "12345678901234567890"}},
		{
			name: "names every bad field",
			cfg:  ddiscord.Config{LiveChannelID: "#announcements", ModsRoleID: "12", GoodbyeEnabled: "yes"},
			bad:  []string{"liveChannelId", "modsRoleId", "goodbyeEnabled"},
		},
		{name: "rejects 16 digits that predate the Discord epoch", cfg: ddiscord.Config{LogChannelID: "1234567890123456"}, bad: []string{"logChannelId"}},
		{name: "rejects 21 digits that cannot be a 64-bit id", cfg: ddiscord.Config{VIPRoleID: "123456789012345678901"}, bad: []string{"vipRoleId"}},
		{name: "rejects a non-digit in a snowflake", cfg: ddiscord.Config{MemberRoleID: "1234567890123456a"}, bad: []string{"memberRoleId"}},
		{
			name: "accepts the log and voice options",
			cfg: ddiscord.Config{
				LogVoiceChannelID: "12345678901234567", LogIgnoredChannels: "12345678901234567,12345678901234568",
				LogIgnoreBots: "off", VoiceCategoryID: "12345678901234567", VoiceNameTemplate: "{owner}", VoiceUserLimit: "0", VoicePrivacyMode: "locked",
			},
		},
		{name: "accepts a padded voice limit", cfg: ddiscord.Config{VoiceUserLimit: " 5"}},
		{
			name: "refuses more than 25 ignored channels",
			cfg:  ddiscord.Config{LogIgnoredChannels: strings.TrimSuffix(strings.Repeat("12345678901234567,", 26), ",")},
			bad:  []string{"logIgnoredChannelIds"},
		},
		{
			name: "names every bad drifted and new field",
			cfg: ddiscord.Config{
				TicketLogChannelID: "x", TicketArchiveCategoryID: "x", SubsChannelID: "x", TicketStaffRoles: "1,2",
				TicketOpenLimit: "9", TicketTranscriptEnabled: "yes", AutoRoleEnabled: "maybe",
				LogMessagesChannelID: "x", LogIgnoredChannels: "nope", LogRolesEnabled: "1",
				VoiceUserLimit: "100", VoicePrivacyMode: "secret",
			},
			bad: []string{
				"ticketLogChannelId", "ticketArchiveCategoryId", "subsChannelId", "ticketStaffRoleIds",
				"ticketOpenLimit", "ticketTranscriptEnabled", "autoRoleEnabled",
				"logMessagesChannelId", "logIgnoredChannelIds", "logRolesEnabled", "voiceUserLimit", "voicePrivacy",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			set := call[discorddata.ConfigSetReply](t, h, discorddata.VerbConfigSet, discorddata.ConfigSetRequest{
				GuildID: guildID, BroadcasterID: broadcasterID, Config: tc.cfg,
			})
			stored := call[discorddata.ConfigGetReply](t, h, discorddata.VerbConfigGet, discorddata.ConfigGetRequest{GuildID: guildID})

			if len(tc.bad) == 0 {
				assert.Equal(t, discorddata.ConfigSetReply{Version: 1}, set)
				assert.Equal(t, discorddata.ConfigGetReply{Config: tc.cfg, Version: 1, Found: true}, stored)
				return
			}
			assert.ElementsMatch(t, tc.bad, set.Fields)
			assert.Equal(t, discorddata.CodeInvalid, set.Code)
			assert.False(t, stored.Found, "a refused config must not be stored")
		})
	}
}

func TestConfigSetRefusesAGuildTheCallerDoesNotOwn(t *testing.T) {
	h := newHarness(t)

	reply := call[discorddata.ConfigSetReply](t, h, discorddata.VerbConfigSet, discorddata.ConfigSetRequest{
		GuildID: guildID, BroadcasterID: broadcasterID + 1,
	})

	assert.Equal(t, discorddata.CodeNotBound, reply.Code)
}
