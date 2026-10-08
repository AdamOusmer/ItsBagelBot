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

func existingChannels(names ...string) []discapi.Snowflake {
	out := make([]discapi.Snowflake, 0, len(names))
	for _, name := range names {
		out = append(out, discapi.Snowflake{ID: "old-" + name, Name: name})
	}
	return out
}

func livedInServer() []discapi.Snowflake {
	var names []string
	for i := range ddiscord.LivingCommunityMinChannels {
		names = append(names, "existing-"+strconv.Itoa(i))
	}
	return existingChannels(append(names, "Clips")...)
}

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
	required := []string{got.LiveChannelID, got.ClipsChannelID, got.VoiceHubID, got.LogChannelID, got.TicketChannelID, got.TicketCategoryID}
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
		name:     "adopts matching channels on a lived-in server and adds only the hub, logs and ticket categories",
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
			ddiscord.RoleOwner: "", ddiscord.RoleLeadMod: "8", ddiscord.RoleMods: strconv.FormatInt(int64(ddiscord.PermModerator), 10),
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

type pinView struct {
	Owner   string
	Mods    string
	VIP     string
	Member  string
	Created []string
	Dropped []string
	Staff   []string
}

func viewPins(got setup.GuildSetupResult, d *fakeDiscord) pinView {
	var staff []string
	for _, o := range d.createdChannels["staff"].PermissionOverwrites {
		staff = append(staff, o.ID)
	}
	return pinView{
		Owner: got.OwnerRoleID, Mods: got.ModsRoleID, VIP: got.VIPRoleID, Member: got.MemberRoleID,
		Created: d.roleNames(), Dropped: got.DroppedPins, Staff: staff,
	}
}

func TestSetupGuildPinnedRoles(t *testing.T) {
	everyRole := []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleMods, ddiscord.RoleVIP, ddiscord.RoleRegulars, ddiscord.RoleMember}
	defaultStaff := []string{"guild-1", "role-owner", "role-lead mod", "role-mods"}
	cases := []struct {
		name string
		live []string
		pins map[string]string
		want pinView
	}{{
		name: "adopts pinned roles instead of creating them and grants them their channels",
		live: []string{"existing-mods", "existing-member"},
		pins: map[string]string{ddiscord.SlotMods: "existing-mods", ddiscord.SlotMember: "existing-member"},
		want: pinView{
			Owner: "role-owner", Mods: "existing-mods", VIP: "role-vip", Member: "existing-member",
			Created: []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleVIP, ddiscord.RoleRegulars},
			Staff:   []string{"guild-1", "role-owner", "role-lead mod", "existing-mods"},
		},
	}, {
		name: "ignores a pin for an unknown slot",
		live: []string{"nope"},
		pins: map[string]string{"janitor": "nope"},
		want: pinView{Owner: "role-owner", Mods: "role-mods", VIP: "role-vip", Member: "role-member", Created: everyRole, Staff: defaultStaff},
	}, {
		name: "drops a pin whose role is gone and creates the role instead",
		pins: map[string]string{ddiscord.SlotMods: "deleted-role"},
		want: pinView{
			Owner: "role-owner", Mods: "role-mods", VIP: "role-vip", Member: "role-member", Created: everyRole,
			Dropped: []string{ddiscord.SlotMods}, Staff: defaultStaff,
		},
	}, {
		name: "reports dropped pins sorted and keeps the pins that are live",
		live: []string{"live-vip"},
		pins: map[string]string{
			ddiscord.SlotMods: "gone-1", ddiscord.SlotOwner: "gone-2", ddiscord.SlotMember: "gone-3", ddiscord.SlotVIP: "live-vip",
		},
		want: pinView{
			Owner: "role-owner", Mods: "role-mods", VIP: "live-vip", Member: "role-member",
			Created: []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleMods, ddiscord.RoleRegulars, ddiscord.RoleMember},
			Dropped: []string{ddiscord.SlotMember, ddiscord.SlotMods, ddiscord.SlotOwner}, Staff: defaultStaff,
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{roles: existingRoles(tc.live)}

			got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), setup.GuildSetupRequest{
				GuildID: "guild-1", BroadcasterID: "42", PinnedRoles: tc.pins,
			})

			require.NoError(t, err)
			require.Equal(t, tc.want, viewPins(got, d))
		})
	}
}

func existingRoles(ids []string) []discapi.Snowflake {
	out := make([]discapi.Snowflake, 0, len(ids))
	for _, id := range ids {
		out = append(out, discapi.Snowflake{ID: id, Name: id})
	}
	return out
}

func TestGuildLayout(t *testing.T) {
	yes, no := true, false
	everyone := setup.GuildEntry{ID: "guild-1", Name: "@everyone"}
	cases := []struct {
		name     string
		caller   string
		botID    string
		channels []discapi.Snowflake
		want     setup.GuildLayout
		wantErr  error
	}{{
		name:     "refuses a broadcaster who does not own the guild",
		caller:   "7",
		channels: []discapi.Snowflake{{ID: "c1", Name: "general"}},
		wantErr:  setup.ErrNotBound,
	}, {
		name:     "lists the channels and roles for the owner",
		caller:   "42",
		channels: []discapi.Snowflake{{ID: "c1", Name: "general"}},
		want:     setup.GuildLayout{Channels: []setup.GuildEntry{{ID: "c1", Name: "general"}}, Roles: []setup.GuildEntry{everyone}},
	}, {
		name:   "flags which text channels the bot can post and embed in",
		caller: "42",
		botID:  "bot",
		channels: []discapi.Snowflake{
			{ID: "c1", Name: "general"},
			{ID: "c2", Name: "quiet", PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "guild-1", Allow: "0", Deny: "2048"}}},
			{ID: "c3", Name: "plain", PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "guild-1", Allow: "0", Deny: "16384"}}},
			{ID: "c4", Name: "lobby", Type: ddiscord.ChannelVoice},
		},
		want: setup.GuildLayout{Channels: []setup.GuildEntry{
			{ID: "c1", Name: "general", CanSend: &yes, CanEmbed: &yes},
			{ID: "c2", Name: "quiet", CanSend: &no, CanEmbed: &no},
			{ID: "c3", Name: "plain", CanSend: &yes, CanEmbed: &no},
			{ID: "c4", Name: "lobby", Type: ddiscord.ChannelVoice},
		}, Roles: []setup.GuildEntry{everyone}},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{channels: tc.channels, everyonePerms: "19456"}
			w := setup.New(setup.Config{Discord: d, Store: boundStore(owners{"guild-1": "42"}), Log: zap.NewNop(), BotID: tc.botID})

			got, err := w.GuildLayout(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: tc.caller})

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestUnbindGuild(t *testing.T) {
	cases := []struct {
		name    string
		caller  string
		wantErr error
		bound   string
	}{
		{name: "refuses a broadcaster who does not own the guild", caller: "7", wantErr: setup.ErrNotBound, bound: "42"},
		{name: "unbinds the owner's guild", caller: "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})

			err := newWorker(&fakeDiscord{}, store).UnbindGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: tc.caller})

			require.ErrorIs(t, err, tc.wantErr)
			owner, _ := store.Broadcaster(context.Background(), discordstore.Guild{ID: "guild-1"})
			require.Equal(t, tc.bound, owner.ID)
		})
	}
}

func TestUnbindStaysIdempotent(t *testing.T) {
	w := newWorker(&fakeDiscord{}, boundStore(owners{"guild-1": "42"}))
	ctx := context.Background()
	req := setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}

	require.NoError(t, w.UnbindGuild(ctx, req), "first unbind")
	require.NoError(t, w.UnbindGuild(ctx, req), "second unbind, want the same silence")
	other := setup.GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}
	require.NoError(t, w.UnbindGuild(ctx, other), "unbinding an unknown guild")
}

func workerFor(d *fakeDiscord, store discordstore.Store, offline bool) *setup.Worker {
	if offline {
		return setup.New(setup.Config{Store: store, Log: zap.NewNop()})
	}
	return newWorker(d, store)
}

func TestPostDiscord(t *testing.T) {
	longest := strings.Repeat("é", 2000)
	cases := []struct {
		name    string
		post    discapi.ChatPost
		offline bool
		wantErr error
		want    []discapi.ChatPost
	}{
		{name: "posts content up to the 2000 rune limit", post: discapi.ChatPost{ChannelID: "chan", Content: longest},
			want: []discapi.ChatPost{{ChannelID: "chan", Content: longest}}},
		{name: "requires a channel", post: discapi.ChatPost{Content: "hi"}, wantErr: discapi.ErrBadRequest},
		{name: "requires content", post: discapi.ChatPost{ChannelID: "chan"}, wantErr: discapi.ErrBadRequest},
		{name: "refuses content past the rune limit", post: discapi.ChatPost{ChannelID: "chan", Content: longest + "é"}, wantErr: discapi.ErrBadRequest},
		{name: "requires a discord client", post: discapi.ChatPost{ChannelID: "chan", Content: "hi"}, offline: true, wantErr: discapi.ErrAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{}

			err := workerFor(d, discordstore.NewMem(), tc.offline).PostDiscord(context.Background(), tc.post.ChannelID, tc.post.Content)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, d.chats)
		})
	}
}
