// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
)

func previewRequest() setup.GuildSetupRequest {
	return setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}
}

func itemsWhere(items []outgressrpc.DiscordTemplateItem, action string) []string {
	var out []string
	for _, it := range items {
		if it.Action == action {
			out = append(out, strings.ToLower(it.Name))
		}
	}
	return out
}

func createdKeys(items []outgressrpc.DiscordTemplateItem) []string {
	var out []string
	for _, it := range items {
		if it.Action == outgressrpc.TemplateActionCreate {
			out = append(out, fmt.Sprintf("%s/%s/%d", it.Kind, strings.ToLower(it.Name), it.Type))
		}
	}
	return out
}

func createdNames(d *fakeDiscord) []string {
	var out []string
	for _, name := range d.roleNames() {
		out = append(out, fmt.Sprintf("%s/%s/0", outgressrpc.TemplateKindRole, strings.ToLower(name)))
	}
	for _, spec := range d.createdSpecs {
		out = append(out, fmt.Sprintf("%s/%s/%d", outgressrpc.TemplateKindChannel, strings.ToLower(spec.Name), spec.Type))
	}
	return out
}

func TestPreviewTemplateMatchesWhatApplyCreates(t *testing.T) {
	cases := []struct {
		name     string
		existing []discapi.Snowflake
		roles    []discapi.Snowflake
		livedIn  bool
	}{
		{name: "fresh server"},
		{name: "partial fill", existing: existingChannels("Welcome", "rules", "now-live", "Community"), roles: []discapi.Snowflake{{ID: "r-mods", Name: "Mods"}}},
		{name: "lived-in server", existing: livedInServer(), livedIn: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := boundStore(owners{"guild-1": "42"})
			d := &fakeDiscord{channels: tc.existing, roles: tc.roles}
			w := newWorker(d, store)

			preview, err := w.PreviewTemplate(ctx, previewRequest())

			require.NoError(t, err)
			require.Equal(t, tc.livedIn, preview.LivedIn)
			require.Empty(t, d.createdRoles)
			require.Empty(t, d.createdChannels)
			require.Empty(t, d.panels)

			_, err = w.SetupGuild(ctx, setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

			require.NoError(t, err)
			require.ElementsMatch(t, createdNames(d), createdKeys(preview.Items))
			if tc.name == "fresh server" {
				require.Contains(t, createdKeys(preview.Items), "channel/welcome/"+strconv.Itoa(ddiscord.ChannelCategory))
				require.Contains(t, createdKeys(preview.Items), "channel/welcome/"+strconv.Itoa(ddiscord.ChannelText))
			}
			after, err := w.PreviewTemplate(ctx, previewRequest())
			require.NoError(t, err)
			require.Empty(t, itemsWhere(after.Items, outgressrpc.TemplateActionCreate))
		})
	}
}

func TestPreviewTemplateAdoptItemsCarryExistingIDs(t *testing.T) {
	d := &fakeDiscord{channels: livedInServer(), roles: []discapi.Snowflake{{ID: "r-mods", Name: "Mods"}}}
	store := boundStore(owners{"guild-1": "42"})

	preview, err := newWorker(d, store).PreviewTemplate(context.Background(), previewRequest())

	require.NoError(t, err)
	require.Contains(t, preview.Items, outgressrpc.DiscordTemplateItem{
		Kind: outgressrpc.TemplateKindChannel, Name: "clips", Parent: "Announcements",
		Action: outgressrpc.TemplateActionAdopt, ID: "old-Clips",
	})
	require.Contains(t, preview.Items, outgressrpc.DiscordTemplateItem{
		Kind: outgressrpc.TemplateKindRole, Name: "Mods", Action: outgressrpc.TemplateActionAdopt, ID: "r-mods",
	})
}

func TestPreviewTemplateLeavesTheBindingAlone(t *testing.T) {
	store := boundStore(owners{"guild-1": "42"})

	_, err := newWorker(&fakeDiscord{}, store).PreviewTemplate(context.Background(), previewRequest())

	require.NoError(t, err)
	guilds, _ := store.GuildsOf(context.Background(), discordstore.Broadcaster{ID: "42"})
	require.Len(t, guilds, 1)
}

func TestPreviewTemplateRefusesGuildsThatAreNotStrictlyOurs(t *testing.T) {
	cases := []struct {
		name  string
		store discordstore.Store
	}{
		{"unbound", discordstore.NewMem()},
		{"bound elsewhere", boundStore(owners{"guild-1": "7"})},
		{"cache only", cacheOnlyStore{boundStore(owners{"guild-1": "42"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{}

			_, err := newWorker(d, tc.store).PreviewTemplate(context.Background(), previewRequest())

			require.Error(t, err)
			require.Empty(t, d.createdChannels)
			require.Zero(t, d.listCalls)
		})
	}
}

func TestPreviewTemplateHonoursSubscribersAndPins(t *testing.T) {
	d := &fakeDiscord{roles: []discapi.Snowflake{{ID: "r-pin", Name: "Boss"}}}
	store := boundStore(owners{"guild-1": "42"})
	w := newWorker(d, store)
	req := previewRequest()
	req.PinnedRoles = map[string]string{ddiscord.SlotMods: "r-pin"}

	off, err := w.PreviewTemplate(context.Background(), req)
	require.NoError(t, err)
	req.Subscribers = true
	on, err := w.PreviewTemplate(context.Background(), req)
	require.NoError(t, err)

	offNames := append(itemsWhere(off.Items, outgressrpc.TemplateActionCreate), itemsWhere(off.Items, outgressrpc.TemplateActionPinned)...)
	onNames := append(itemsWhere(on.Items, outgressrpc.TemplateActionCreate), itemsWhere(on.Items, outgressrpc.TemplateActionPinned)...)
	for _, name := range []string{"subscriber", "subscribers", "sub-chat", "sub-media"} {
		require.NotContains(t, offNames, name)
		require.Contains(t, onNames, name)
	}
	require.Contains(t, off.Items, outgressrpc.DiscordTemplateItem{
		Kind: outgressrpc.TemplateKindRole, Name: "Mods", Action: outgressrpc.TemplateActionPinned, ID: "r-pin",
	})
	require.NotContains(t, off.Items, outgressrpc.DiscordTemplateItem{
		Kind: outgressrpc.TemplateKindRole, Name: "Mods", Action: outgressrpc.TemplateActionCreate,
	})
}
