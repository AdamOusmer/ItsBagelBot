// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fillView struct {
	Refused bool
	Filled  bool
	Live    string
	Clips   string
	Desk    string
	Created bool
	BoundTo string
	Guilds  int
}

func viewFill(got setup.GuildSetupResult, d *fakeDiscord, store *discordstore.Mem) fillView {
	ctx := context.Background()
	owner, _ := store.Broadcaster(ctx, discordstore.Guild{ID: "guild-1"})
	guilds, _ := store.GuildsOf(ctx, discordstore.Broadcaster{ID: "42"})
	required := []string{got.LiveChannelID, got.ClipsChannelID, got.VoiceHubID, got.VoiceCategoryID, got.LogChannelID, got.TicketChannelID, got.TicketCategoryID}
	view := fillView{
		Refused: got.Refused != "", Filled: !slices.Contains(required, ""),
		Live: got.LiveChannelID, Clips: got.ClipsChannelID,
		Created: len(d.createdChannels) > 0, BoundTo: owner.ID, Guilds: len(guilds),
	}
	for _, p := range d.panels {
		view.Desk = p.Post.ChannelID
	}
	return view
}

func TestSetupGuildOnLivedInServerAddsOnlyTheBoundChannels(t *testing.T) {
	d := &fakeDiscord{channels: livedInServer()}

	got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

	require.NoError(t, err)
	require.NotEmpty(t, got.Refused)
	require.Equal(t, "ch-+ create voice", got.VoiceHubID)
	require.Equal(t, "ch-voice", got.VoiceCategoryID)
	require.Equal(t, "ch-logs", got.LogChannelID)
	require.Equal(t, "ch-tickets", got.TicketCategoryID)
	require.Equal(t, "ch-archive", got.TicketArchiveCategoryID)
	require.Empty(t, d.createdRoles)
	for _, name := range []string{"welcome", "now-live", "support", "vip-lounge", "vip"} {
		require.NotContains(t, d.createdChannels, name)
	}
	require.Equal(t, "old-Clips", got.ClipsChannelID)
	require.NotContains(t, d.createdChannels, "chat")
	require.NotContains(t, d.createdChannels, "off-topic")
}

func TestSetupGuildGatedChannelsAdmitTheBot(t *testing.T) {
	d := &fakeDiscord{}
	w := setup.New(setup.Config{Discord: d, Store: discordstore.NewMem(), Log: zap.NewNop(), BotID: "bot-1"})

	_, err := w.SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

	require.NoError(t, err)
	overwrites := d.createdChannels["logs"].PermissionOverwrites
	bot := discapi.PermissionOverwrite{ID: "bot-1", Type: 1, Allow: "52224", Deny: "0"}
	require.Equal(t, bot, overwrites[len(overwrites)-1])
	require.Contains(t, d.createdChannels["now-live"].PermissionOverwrites, bot)
}

func TestSetupGuild(t *testing.T) {
	cases := []struct {
		name     string
		existing []discapi.Snowflake
		owners   owners
		wantErr  error
		want     fillView
	}{{
		name: "fills a fresh server, binds it and posts the ticket desk",
		want: fillView{Filled: true, Live: "ch-now-live", Clips: "ch-clips", Desk: "ch-support", Created: true, BoundTo: "42", Guilds: 1},
	}, {
		name:     "adopts matching channels on a lived-in server and adds only the voice hub and category, logs and ticket categories",
		existing: livedInServer(),
		want:     fillView{Refused: true, Clips: "old-Clips", Created: true, BoundTo: "42", Guilds: 1},
	}, {
		name:     "completes a partial fill by reusing the channels it finds",
		existing: existingChannels("Welcome", "welcome", "rules", "Announcements", "now-live", "clips", "announcements", "Community"),
		owners:   owners{"guild-1": "42"},
		want:     fillView{Filled: true, Live: "old-now-live", Clips: "old-clips", Desk: "ch-support", Created: true, BoundTo: "42", Guilds: 1},
	}, {
		name:    "refuses a guild bound to another broadcaster before any write",
		owners:  owners{"guild-1": "7"},
		wantErr: setup.ErrGuildBoundElsewhere,
		want:    fillView{BoundTo: "7"},
	}, {
		name:   "binds a second guild for the same broadcaster",
		owners: owners{"guild-0": "42"},
		want:   fillView{Filled: true, Live: "ch-now-live", Clips: "ch-clips", Desk: "ch-support", Created: true, BoundTo: "42", Guilds: 2},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{channels: tc.existing}
			store := boundStore(tc.owners)

			got, err := newWorker(d, store).SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, viewFill(got, d, store))
		})
	}
}

func TestSetupGuildBindsTheWelcomeTextChannelNotItsCategory(t *testing.T) {
	d := &fakeDiscord{}

	got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	require.NoError(t, err)

	var welcome []discapi.Snowflake
	for _, ch := range d.channels {
		if strings.EqualFold(ch.Name, "welcome") {
			welcome = append(welcome, ch)
		}
	}
	require.Len(t, welcome, 2)
	text := welcome[slices.IndexFunc(welcome, func(ch discapi.Snowflake) bool { return ch.Type == ddiscord.ChannelText })]
	require.Equal(t, text.ID, got.WelcomeChannelID)
}

type templateFill struct {
	RolePermissions   map[string]string
	ArchiveType       int
	ArchiveOverwrites []discapi.PermissionOverwrite
	DeskButtons       []discapi.Button
}

func TestSetupGuildCreatesTheTemplateRolesStaffArchiveAndDesk(t *testing.T) {
	d := &fakeDiscord{}

	_, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	require.NoError(t, err)

	perms := map[string]string{}
	for _, r := range d.createdRoles {
		perms[r.Name] = r.Permissions
	}
	staffOnly := func(role string) discapi.PermissionOverwrite {
		return discapi.PermissionOverwrite{ID: role, Allow: "1024", Deny: "2048"}
	}
	archive := d.createdChannels["archive"]
	require.Equal(t, templateFill{
		RolePermissions: map[string]string{
			ddiscord.RoleOwner: "", ddiscord.RoleLeadMod: "", ddiscord.RoleMods: strconv.FormatInt(int64(ddiscord.PermModerator)&ddiscord.BotPermissions, 10),
			ddiscord.RoleVIP: "", ddiscord.RoleRegulars: "", ddiscord.RoleMember: "",
		},
		ArchiveType: ddiscord.ChannelCategory,
		ArchiveOverwrites: []discapi.PermissionOverwrite{
			{ID: "guild-1", Allow: "0", Deny: "1024"},
			staffOnly("role-owner"), staffOnly("role-lead mod"), staffOnly("role-mods"),
		},
		DeskButtons: []discapi.Button{{Style: discapi.ButtonPrimary, Label: ddiscord.TicketPanelButtonDefault, CustomID: discapi.CustomTicketOpen}},
	}, templateFill{
		RolePermissions: perms, ArchiveType: archive.Type, ArchiveOverwrites: archive.PermissionOverwrites,
		DeskButtons: d.panels[0].Buttons,
	})
}
