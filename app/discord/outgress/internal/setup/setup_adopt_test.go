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
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
)

func adoptRequest() setup.GuildSetupRequest {
	return setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42", Mode: outgressrpc.SetupModeAdopt}
}

func TestSetupGuildAdoptModeOnEmptyGuildOnlyBinds(t *testing.T) {
	d := &fakeDiscord{}
	store := discordstore.NewMem()

	got, err := newWorker(d, store).SetupGuild(context.Background(), adoptRequest())

	require.NoError(t, err)
	owner, _ := store.Broadcaster(context.Background(), discordstore.Guild{ID: "guild-1"})
	require.Equal(t, "42", owner.ID)
	require.Empty(t, d.createdRoles)
	require.Empty(t, d.createdChannels)
	require.Empty(t, d.panels)
	require.Empty(t, got.Refused)
	require.Equal(t, setup.GuildSetupResult{DiscordSetupIDs: outgressrpc.DiscordSetupIDs{GuildID: "guild-1"}}, got)
}

func TestSetupGuildAdoptModeFillsFromExistingResources(t *testing.T) {
	d := &fakeDiscord{
		channels: existingChannels("now-live", "Clips", "support"),
		roles:    []discapi.Snowflake{{ID: "r-mods", Name: "Mods"}, {ID: "r-pin", Name: "Boss"}, {ID: "r-gone-not", Name: "other"}},
	}
	req := adoptRequest()
	req.PinnedRoles = map[string]string{ddiscord.SlotMods: "r-pin", ddiscord.SlotVIP: "r-missing"}

	got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, "old-now-live", got.LiveChannelID)
	require.Equal(t, "old-Clips", got.ClipsChannelID)
	require.Equal(t, "old-support", got.TicketChannelID)
	require.Equal(t, "r-pin", got.ModsRoleID)
	require.Empty(t, got.VIPRoleID)
	require.Equal(t, []string{ddiscord.SlotVIP}, got.DroppedPins)
	require.Empty(t, d.createdRoles)
	require.Empty(t, d.createdChannels)
	require.Empty(t, d.panels)
}

func TestSetupGuildAdoptModeBindsTheWelcomeTextChannelNotItsCategory(t *testing.T) {
	d := &fakeDiscord{channels: []discapi.Snowflake{
		{ID: "cat-welcome", Name: "Welcome", Type: ddiscord.ChannelCategory},
		{ID: "text-welcome", Name: "Welcome", Type: ddiscord.ChannelText},
	}}

	got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), adoptRequest())

	require.NoError(t, err)
	require.Equal(t, "text-welcome", got.WelcomeChannelID)
	require.Empty(t, d.createdChannels)
}
