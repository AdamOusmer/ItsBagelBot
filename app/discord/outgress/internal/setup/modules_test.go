// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

func liveRow(channel string) ddiscord.ModuleRow {
	raw, _ := codec.Marshal(ddiscord.LiveSettings{LiveChannelID: channel})
	return ddiscord.ModuleRow{Name: ddiscord.ModuleLive, Enabled: true, Settings: raw}
}

func storedRow(store *discordstore.Mem, name string) ddiscord.ModuleRow {
	mods, _ := store.GuildModules(context.Background(), discordstore.Guild{ID: "guild-1"})
	return mods.Rows[name]
}

func TestGuildModules(t *testing.T) {
	cases := []struct {
		name    string
		req     setup.GuildSetupRequest
		found   bool
		wantErr error
	}{
		{name: "splits the legacy config into module rows", req: setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}, found: true},
		{name: "refuses another broadcaster's guild", req: setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "99"}, wantErr: setup.ErrNotBound},
		{name: "refuses an unbound guild", req: setup.GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}, wantErr: setup.ErrNotBound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})
			store.PutGuildConfig(discordstore.Guild{ID: "guild-1"}, ddiscord.Config{LiveChannelID: "123"})

			mods, found, err := newWorker(&fakeDiscord{}, store).GuildModules(context.Background(), tc.req)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.found, found)
			if tc.found {
				var live ddiscord.LiveSettings
				require.NoError(t, mods.Decode(ddiscord.ModuleLive, &live))
				require.Equal(t, "123", live.LiveChannelID)
			}
		})
	}
}

func TestSetGuildModule(t *testing.T) {
	cases := []struct {
		name    string
		write   setup.GuildModuleWrite
		offline bool
		version int
		wantErr error
	}{
		{name: "saves the first version and reads back", write: setup.GuildModuleWrite{BroadcasterID: "42", Row: liveRow("123")}, version: 1},
		{name: "refuses a stale expected version", write: setup.GuildModuleWrite{BroadcasterID: "42", Row: liveRow("123"), ExpectedVersion: 3}, wantErr: discordstore.ErrConfigConflict},
		{name: "refuses another broadcaster's guild", write: setup.GuildModuleWrite{BroadcasterID: "99", Row: liveRow("123")}, wantErr: setup.ErrNotBound},
		{name: "refuses a channel from another guild", write: setup.GuildModuleWrite{BroadcasterID: "42", Row: liveRow("foreign")}, wantErr: discapi.ErrForbidden},
		{name: "refuses channels it cannot verify without a discord client", write: setup.GuildModuleWrite{BroadcasterID: "42", Row: liveRow("123")}, offline: true, wantErr: setup.ErrDiscordUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})
			write := tc.write
			write.GuildID = "guild-1"

			version, err := workerFor(&fakeDiscord{channels: []discapi.Snowflake{{ID: "123"}}}, store, tc.offline).SetGuildModule(context.Background(), write)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.version, version)
			require.Equal(t, tc.version, storedRow(store, ddiscord.ModuleLive).Version)
		})
	}
}

func TestSetGuildModuleChecksOnlyItsOwnChannels(t *testing.T) {
	store := boundStore(owners{"guild-1": "42"})
	raw, _ := codec.Marshal(ddiscord.ClipsSettings{})
	row := ddiscord.ModuleRow{Name: ddiscord.ModuleClips, Settings: raw}
	d := &fakeDiscord{}

	_, err := newWorker(d, store).SetGuildModule(context.Background(), setup.GuildModuleWrite{GuildID: "guild-1", BroadcasterID: "42", Row: row})

	require.NoError(t, err)
	require.Zero(t, d.listCalls)
}

func TestSetGuildModuleIgnoresStaleStoredChannels(t *testing.T) {
	cases := []struct {
		name    string
		channel string
		wantErr error
	}{
		{name: "keeps a stored channel the guild no longer has", channel: "stale"},
		{name: "refuses a new channel the guild does not have", channel: "unknown", wantErr: discapi.ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})
			store.PutGuildModules(discordstore.Guild{ID: "guild-1"}, ddiscord.GuildModules{Rows: map[string]ddiscord.ModuleRow{ddiscord.ModuleLive: liveRow("stale")}})
			write := setup.GuildModuleWrite{GuildID: "guild-1", BroadcasterID: "42", Row: liveRow(tc.channel)}

			_, err := newWorker(&fakeDiscord{channels: []discapi.Snowflake{{ID: "123"}}}, store).SetGuildModule(context.Background(), write)

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
