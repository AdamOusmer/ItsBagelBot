// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleSetStoresAndListsARow(t *testing.T) {
	h := newHarness(t)

	set := call[discorddata.ModuleSetReply](t, h, discorddata.VerbModuleSet, discorddata.ModuleSetRequest{
		GuildID: guildID, BroadcasterID: broadcasterID, Name: ddiscord.ModuleClips, Enabled: true,
		Settings: codec.RawMessage(`{"clipsChannelId":"12345678901234567"}`),
	})
	assert.Equal(t, discorddata.ModuleSetReply{Version: 1}, set)

	list := call[discorddata.ModuleListReply](t, h, discorddata.VerbModuleList, discorddata.ModuleListRequest{GuildID: guildID})
	require.True(t, list.Found)
	require.Len(t, list.Modules, 1)
	assert.Equal(t, ddiscord.ModuleClips, list.Modules[0].Name)
	assert.Equal(t, 1, list.Modules[0].Version)
	assert.JSONEq(t, `{"clipsChannelId":"12345678901234567"}`, string(list.Modules[0].Settings))
}

func TestModuleSetRejectsInvalidRequestsBeforeStoring(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  discorddata.ModuleSetRequest
		bad  []string
	}{
		{
			name: "bad guild snowflake",
			req:  discorddata.ModuleSetRequest{GuildID: "12", BroadcasterID: broadcasterID, Name: ddiscord.ModuleLive},
			bad:  []string{"guildId"},
		},
		{
			name: "empty guild",
			req:  discorddata.ModuleSetRequest{BroadcasterID: broadcasterID, Name: ddiscord.ModuleLive},
			bad:  []string{"guildId"},
		},
		{
			name: "unknown module",
			req:  discorddata.ModuleSetRequest{GuildID: guildID, BroadcasterID: broadcasterID, Name: "nope"},
			bad:  []string{"name"},
		},
		{
			name: "identity has no settings",
			req:  discorddata.ModuleSetRequest{GuildID: guildID, BroadcasterID: broadcasterID, Name: ddiscord.ModuleIdentity},
			bad:  []string{"name"},
		},
		{
			name: "bad field in the settings",
			req: discorddata.ModuleSetRequest{
				GuildID: guildID, BroadcasterID: broadcasterID, Name: ddiscord.ModuleLive,
				Settings: codec.RawMessage(`{"liveChannelId":"#announcements"}`),
			},
			bad: []string{"liveChannelId"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			set := call[discorddata.ModuleSetReply](t, h, discorddata.VerbModuleSet, tc.req)
			list := call[discorddata.ModuleListReply](t, h, discorddata.VerbModuleList, discorddata.ModuleListRequest{GuildID: guildID})

			assert.Equal(t, tc.bad, set.Fields)
			assert.Equal(t, discorddata.CodeInvalid, set.Code)
			assert.False(t, list.Found, "a refused module must not be stored")
		})
	}
}

func TestModuleSetMapsStoreErrors(t *testing.T) {
	h := newHarness(t)
	req := discorddata.ModuleSetRequest{GuildID: guildID, BroadcasterID: broadcasterID, Name: ddiscord.ModuleLive}

	call[discorddata.ModuleSetReply](t, h, discorddata.VerbModuleSet, req)
	conflict := call[discorddata.ModuleSetReply](t, h, discorddata.VerbModuleSet, req)
	assert.Equal(t, discorddata.CodeConflict, conflict.Code)

	req.BroadcasterID++
	req.ExpectedVersion = 1
	foreign := call[discorddata.ModuleSetReply](t, h, discorddata.VerbModuleSet, req)
	assert.Equal(t, discorddata.CodeNotBound, foreign.Code)
}

func TestModuleListRejectsABadGuildID(t *testing.T) {
	h := newHarness(t)

	reply := call[discorddata.ModuleListReply](t, h, discorddata.VerbModuleList, discorddata.ModuleListRequest{GuildID: "x"})

	assert.Equal(t, discorddata.CodeInvalid, reply.Code)
}
