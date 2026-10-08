// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
)

type storedConfig struct {
	Config  ddiscord.Config
	Version int
	Found   bool
}

func readConfig(store *discordstore.Mem) storedConfig {
	cfg, version, found := store.GuildConfig(context.Background(), discordstore.Guild{ID: "guild-1"})
	return storedConfig{Config: cfg, Version: version, Found: found}
}

func TestGuildConfig(t *testing.T) {
	saved := ddiscord.Config{LiveChannelID: "123"}
	cases := []struct {
		name    string
		req     setup.GuildSetupRequest
		want    storedConfig
		wantErr error
	}{
		{name: "reads back the saved config and its version", req: setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"},
			want: storedConfig{Config: saved, Version: 1, Found: true}},
		{name: "refuses another broadcaster's guild", req: setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "99"}, wantErr: setup.ErrNotBound},
		{name: "refuses an unbound guild", req: setup.GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}, wantErr: setup.ErrNotBound},
		{name: "refuses a caller with no broadcaster", req: setup.GuildSetupRequest{GuildID: "guild-1"}, wantErr: setup.ErrNotBound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})
			store.PutGuildConfig(discordstore.Guild{ID: "guild-1"}, saved)

			cfg, version, found, err := newWorker(&fakeDiscord{}, store).GuildConfig(context.Background(), tc.req)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, storedConfig{Config: cfg, Version: version, Found: found})
		})
	}
}

type configWrite struct {
	Version int
	Stored  storedConfig
}

func TestSetGuildConfig(t *testing.T) {
	live := ddiscord.Config{LiveChannelID: "123"}
	cases := []struct {
		name    string
		write   setup.GuildConfigWrite
		offline bool
		want    configWrite
		wantErr error
	}{{
		name:  "saves the first version and reads back",
		write: setup.GuildConfigWrite{BroadcasterID: "42", Config: live},
		want:  configWrite{Version: 1, Stored: storedConfig{Config: live, Version: 1, Found: true}},
	}, {
		name:    "refuses a stale expected version",
		write:   setup.GuildConfigWrite{BroadcasterID: "42", ExpectedVersion: 1},
		wantErr: discordstore.ErrConfigConflict,
	}, {
		name:    "refuses another broadcaster's guild",
		write:   setup.GuildConfigWrite{BroadcasterID: "99"},
		wantErr: setup.ErrNotBound,
	}, {
		name:    "refuses a config that names another guild",
		write:   setup.GuildConfigWrite{BroadcasterID: "42", Config: ddiscord.Config{GuildID: "guild-2"}},
		wantErr: discapi.ErrForbidden,
	}, {
		name:    "refuses channels it cannot verify without a discord client",
		write:   setup.GuildConfigWrite{BroadcasterID: "42", Config: live},
		offline: true,
		wantErr: setup.ErrDiscordUnavailable,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})
			write := tc.write
			write.GuildID = "guild-1"

			version, err := workerFor(&fakeDiscord{}, store, tc.offline).SetGuildConfig(context.Background(), write)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, configWrite{Version: version, Stored: readConfig(store)})
		})
	}
}

func TestConfigRejectsForeignChannelsBeforeSave(t *testing.T) {
	for _, configure := range []func(*ddiscord.Config){
		func(c *ddiscord.Config) { c.LiveChannelID = "foreign" },
		func(c *ddiscord.Config) { c.ClipsChannelID = "foreign" },
		func(c *ddiscord.Config) { c.WelcomeChannelID = "foreign" },
		func(c *ddiscord.Config) { c.LogChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketLogChannelID = "foreign" },
		func(c *ddiscord.Config) { c.TicketArchiveCategoryID = "foreign" },
		func(c *ddiscord.Config) { c.VoiceCategoryID = "foreign" },
		func(c *ddiscord.Config) { c.LogVoiceChannelID = "foreign" },
		func(c *ddiscord.Config) { c.LogIgnoredChannels = "foreign" },
	} {
		store := boundStore(owners{"guild-1": "42"})
		w := newWorker(&fakeDiscord{channelGuilds: map[string]string{"foreign": "guild-2"}}, store)
		cfg := ddiscord.Config{}
		configure(&cfg)
		_, err := w.SetGuildConfig(context.Background(), setup.GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "42", Config: cfg})
		require.ErrorIs(t, err, discapi.ErrForbidden, "expected refusal")
		require.False(t, readConfig(store).Found, "foreign configuration persisted")
	}
}

func guildIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, "guild-"+strconv.Itoa(i))
	}
	return ids
}

type listingView struct {
	Listed    int
	Truncated bool
	Present   int
	Lookups   int
	First     setup.GuildSummary
	Nil       bool
}

func viewListing(got setup.GuildListing, d *fakeDiscord) listingView {
	view := listingView{Listed: len(got.Guilds), Truncated: got.Truncated, Lookups: d.guildLookups, Nil: got.Guilds == nil}
	for _, g := range got.Guilds {
		if g.BotPresent {
			view.Present++
		}
	}
	if len(got.Guilds) > 0 {
		view.First = got.Guilds[0]
	}
	return view
}

func listingWorker(d *fakeDiscord, bound []string, unavailable bool) *setup.Worker {
	bindings := owners{}
	for _, id := range bound {
		bindings[id] = "42"
	}
	var store discordstore.Store = boundStore(bindings)
	if unavailable {
		store = unavailableStore{Store: store}
	}
	return newWorker(d, store)
}

func listingContext(t *testing.T, cancelled bool) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if cancelled {
		cancel()
	}
	return ctx
}

func TestListGuilds(t *testing.T) {
	present := setup.GuildSummary{
		GuildID: "guild-1", Name: "server guild-1", IconURL: "https://cdn.discordapp.com/icons/guild-1/abc.png",
		MemberCount: 42, BotPresent: true,
	}
	cases := []struct {
		name        string
		bound       []string
		guildErr    error
		unavailable bool
		cancelled   bool
		want        listingView
		wantErr     error
	}{{
		name:  "lists every connected server",
		bound: []string{"guild-1", "guild-2"},
		want:  listingView{Listed: 2, Present: 2, Lookups: 2, First: present},
	}, {
		name:     "keeps a guild the bot was kicked from",
		bound:    []string{"guild-1"},
		guildErr: discapi.ErrForbidden,
		want:     listingView{Listed: 1, Lookups: 1, First: setup.GuildSummary{GuildID: "guild-1"}},
	}, {
		name:     "keeps a guild whose lookup failed for another reason",
		bound:    []string{"guild-1"},
		guildErr: errors.New("discord 500"),
		want:     listingView{Listed: 1, Lookups: 1, First: setup.GuildSummary{GuildID: "guild-1"}},
	}, {
		name:        "fails when the store cannot say",
		unavailable: true,
		want:        listingView{Nil: true},
		wantErr:     discordstore.ErrStoreUnavailable,
	}, {
		name:  "caps the listing and the REST burst",
		bound: guildIDs(setup.MaxListedGuilds + 3),
		want: listingView{Listed: setup.MaxListedGuilds, Truncated: true, Present: setup.MaxListedGuilds, Lookups: setup.MaxListedGuilds,
			First: setup.GuildSummary{GuildID: "guild-0", Name: "server guild-0", IconURL: "https://cdn.discordapp.com/icons/guild-0/abc.png", MemberCount: 42, BotPresent: true}},
	}, {
		name:  "TestExactlyMaxListedGuildsIsNotTruncated",
		bound: guildIDs(setup.MaxListedGuilds),
		want: listingView{Listed: setup.MaxListedGuilds, Present: setup.MaxListedGuilds, Lookups: setup.MaxListedGuilds,
			First: setup.GuildSummary{GuildID: "guild-0", Name: "server guild-0", IconURL: "https://cdn.discordapp.com/icons/guild-0/abc.png", MemberCount: 42, BotPresent: true}},
	}, {
		name:      "returns what it has when the caller gives up",
		bound:     []string{"guild-1", "guild-2"},
		cancelled: true,
		wantErr:   context.Canceled,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{guildErr: tc.guildErr}

			got, err := listingWorker(d, tc.bound, tc.unavailable).ListGuilds(listingContext(t, tc.cancelled), "42")

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, viewListing(got, d))
		})
	}
}

func TestEveryDashboardVerbRefusesACacheOnlyBinding(t *testing.T) {
	store := discordstore.NewMem()
	bind := discordstore.Binding{
		Guild:       discordstore.Guild{ID: "guild-1"},
		Broadcaster: discordstore.Broadcaster{ID: "42"},
	}
	require.NoError(t, store.BindGuild(context.Background(), bind))
	w := newWorker(&fakeDiscord{}, cacheOnlyStore{Store: store})
	ctx := context.Background()
	req := setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}

	cases := []struct {
		name string
		run  func() error
	}{
		{"layout", func() error { _, err := w.GuildLayout(ctx, req); return err }},
		{"status", func() error { _, err := w.GuildInfo(ctx, req); return err }},
		{"unbind", func() error { return w.UnbindGuild(ctx, req) }},
		{"desk.repost", func() error {
			_, err := w.RepostDesk(ctx, setup.DeskRepostRequest{
				GuildID: "guild-1", BroadcasterID: "42", ChannelID: "chan-1",
			})
			return err
		}},
		{"config.read", func() error { _, _, _, err := w.GuildConfig(ctx, req); return err }},
		{"config.write", func() error {
			_, err := w.SetGuildConfig(ctx, setup.GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "42"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.run(), discordstore.ErrStoreUnavailable)
		})
	}
}
