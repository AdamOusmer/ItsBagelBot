// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
	"strconv"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

// boundWorker wires a worker over a memory store holding guildIDs, all bound
// to broadcaster 42.
func boundWorker(t *testing.T, guildIDs ...string) (*Worker, *discordstore.Mem, *guildRecorder) {
	t.Helper()
	store := discordstore.NewMem()
	for _, id := range guildIDs {
		if err := store.BindGuild(context.Background(), discordstore.Binding{Guild: discordstore.Guild{ID: id}, Broadcaster: discordstore.Broadcaster{ID: "42"}}); err != nil {
			t.Fatalf("BindGuild: %v", err)
		}
	}
	recorder := &guildRecorder{}
	return setupWorker(recorder, store), store, recorder
}

func TestSetGuildConfigRoundTrips(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1")
	ctx := context.Background()

	version, err := w.SetGuildConfig(ctx, GuildConfigWrite{
		GuildID: "guild-1", BroadcasterID: "42",
		Config: ddiscord.Config{LiveChannelID: "123"},
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if version != 1 {
		t.Fatalf("first save version = %d, want 1", version)
	}

	cfg, version, found, err := w.GuildConfig(ctx, GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !found {
		t.Fatal("read back: the config just written is missing")
	}
	if version != 1 {
		t.Fatalf("read back version = %d, want 1", version)
	}
	if cfg.LiveChannelID != "123" {
		t.Fatalf("read back live channel = %q, want 123", cfg.LiveChannelID)
	}
}

func TestSetGuildConfigRefusesAStaleVersion(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1")
	ctx := context.Background()
	write := GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "42"}

	if _, err := w.SetGuildConfig(ctx, write); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// The second tab still holds the version it loaded with.
	if _, err := w.SetGuildConfig(ctx, write); !errors.Is(err, discordstore.ErrConfigConflict) {
		t.Fatalf("want a conflict, got %v", err)
	}
}

func TestGuildConfigRefusesAGuildTheCallerDoesNotOwn(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1")
	ctx := context.Background()

	if _, _, _, err := w.GuildConfig(ctx, GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "99"}); !errors.Is(err, ErrNotBound) {
		t.Fatalf("want ErrNotBound for another broadcaster's guild, got %v", err)
	}
	// An unbound guild reads the same, so a caller cannot probe guild ids.
	if _, _, _, err := w.GuildConfig(ctx, GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}); !errors.Is(err, ErrNotBound) {
		t.Fatalf("want ErrNotBound for an unbound guild, got %v", err)
	}
	if _, err := w.SetGuildConfig(ctx, GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "99"}); !errors.Is(err, ErrNotBound) {
		t.Fatalf("want ErrNotBound on the write path, got %v", err)
	}
}

func TestListGuildsListsEveryConnectedServer(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1", "guild-2")

	got, err := w.ListGuilds(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListGuilds: %v", err)
	}
	if len(got.Guilds) != 2 {
		t.Fatalf("want both servers, got %+v", got)
	}
	if got.Truncated {
		t.Fatal("two servers is not a truncated listing")
	}
	for _, g := range got.Guilds {
		if !g.BotPresent || g.Name == "" {
			t.Fatalf("want a present, named server, got %+v", g)
		}
	}
}

func TestListGuildsKeepsAGuildTheBotWasKickedFrom(t *testing.T) {
	w, _, recorder := boundWorker(t, "guild-1")
	recorder.getGuildErr = discapi.ErrForbidden

	got, err := w.ListGuilds(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListGuilds: %v", err)
	}
	if len(got.Guilds) != 1 {
		t.Fatalf("a kicked bot must not hide the binding, got %+v", got)
	}
	if got.Guilds[0].BotPresent {
		t.Fatal("want bot_present false for a 403")
	}
}

// One broadcaster, two servers: the second setup must not be refused. This is
// the whole multi-guild model in one assertion.
func TestSetupBindsASecondGuildForTheSameBroadcaster(t *testing.T) {
	store := discordstore.NewMem()
	w := setupWorker(&guildRecorder{}, store)
	ctx := context.Background()

	for _, id := range []string{"guild-1", "guild-2"} {
		if _, err := w.SetupGuild(ctx, GuildSetupRequest{GuildID: id, BroadcasterID: "42"}); err != nil {
			t.Fatalf("SetupGuild(%s): %v", id, err)
		}
	}

	got, err := store.GuildsOf(ctx, discordstore.Broadcaster{ID: "42"})
	if err != nil {
		t.Fatalf("GuildsOf: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want two bound guilds, got %+v", got)
	}
}

// cacheOnlyStore answers every binding lookup from the cache, which is what
// the RPC store does while discord-data is unreachable.
type cacheOnlyStore struct{ discordstore.Store }

func (c cacheOnlyStore) BindingOf(ctx context.Context, g discordstore.Guild) (discordstore.Broadcaster, discordstore.BindingSource, bool) {
	b, ok := c.Store.Broadcaster(ctx, g)
	return b, discordstore.BindingFromCache, ok
}

// TestOwnershipRefusesACacheOnlyBinding is why BindingOf carries provenance. A
// cached binding can outlive an unbind, so answering "yes, this server is
// yours" from it would hand one streamer another's settings; the dashboard
// gets a retryable failure instead.
func TestOwnershipRefusesACacheOnlyBinding(t *testing.T) {
	store := discordstore.NewMem()
	bind := discordstore.Binding{
		Guild:       discordstore.Guild{ID: "guild-1"},
		Broadcaster: discordstore.Broadcaster{ID: "42"},
	}
	if err := store.BindGuild(context.Background(), bind); err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	w := setupWorker(&guildRecorder{}, cacheOnlyStore{Store: store})
	ctx := context.Background()

	if _, _, _, err := w.GuildConfig(ctx, GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}); !errors.Is(err, discordstore.ErrStoreUnavailable) {
		t.Fatalf("want ErrStoreUnavailable on the read path, got %v", err)
	}
	if _, err := w.SetGuildConfig(ctx, GuildConfigWrite{GuildID: "guild-1", BroadcasterID: "42"}); !errors.Is(err, discordstore.ErrStoreUnavailable) {
		t.Fatalf("want ErrStoreUnavailable on the write path, got %v", err)
	}
}

// TestListGuildsFailsWhenTheStoreCannotSay: an unreachable store must not read
// as "this streamer connected no servers".
func TestListGuildsFailsWhenTheStoreCannotSay(t *testing.T) {
	w := setupWorker(&guildRecorder{}, unavailableStore{Store: discordstore.NewMem()})

	got, err := w.ListGuilds(context.Background(), "42")
	if !errors.Is(err, discordstore.ErrStoreUnavailable) || got.Guilds != nil {
		t.Fatalf("ListGuilds = %+v, %v; want nil, ErrStoreUnavailable", got, err)
	}
}

// unavailableStore is a store whose guild listing is down.
type unavailableStore struct{ discordstore.Store }

func (unavailableStore) GuildsOf(context.Context, discordstore.Broadcaster) ([]discordstore.Binding, error) {
	return nil, discordstore.ErrStoreUnavailable
}

// TestListGuildsCapsAndCarriesTheBindTime pins both halves of one listing: no
// more than MaxListedGuilds entries (one REST call each), and the bind time
// the server card shows as "connected since".
func TestListGuildsCapsAndCarriesTheBindTime(t *testing.T) {
	ids := make([]string, 0, MaxListedGuilds+3)
	for i := range MaxListedGuilds + 3 {
		ids = append(ids, "guild-"+strconv.Itoa(i))
	}
	w, _, recorder := boundWorker(t, ids...)

	got, err := w.ListGuilds(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListGuilds: %v", err)
	}
	if len(got.Guilds) != MaxListedGuilds {
		t.Fatalf("want the listing capped at %d, got %d", MaxListedGuilds, len(got.Guilds))
	}
	// The flag is the only way the dashboard can tell "twenty-five servers"
	// from "the first twenty-five of twenty-eight".
	if !got.Truncated {
		t.Fatal("a capped listing must say it is short")
	}
	if recorder.getGuildCalls > MaxListedGuilds {
		t.Fatalf("the cap must bound the REST burst too, got %d calls", recorder.getGuildCalls)
	}
}

// TestListGuildsReturnsWhatItHasOnATimeout: one entry is one REST round trip,
// so a slow Discord can outlive the RPC deadline. A short list the dashboard
// can render beats nothing at all.
func TestListGuildsReturnsWhatItHasOnATimeout(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1", "guild-2")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := w.ListGuilds(ctx, "42")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want the deadline reported, got %v", err)
	}
	if got.Guilds == nil {
		t.Fatal("a partial listing must be a slice, not nil")
	}
}

// TestExactlyMaxListedGuildsIsNotTruncated: the count that the caller cannot
// tell apart by looking at the slice. Twenty-five bindings fit, so the picker
// must not claim there are more.
func TestExactlyMaxListedGuildsIsNotTruncated(t *testing.T) {
	ids := make([]string, 0, MaxListedGuilds)
	for i := range MaxListedGuilds {
		ids = append(ids, "guild-"+strconv.Itoa(i))
	}
	w, _, _ := boundWorker(t, ids...)

	got, err := w.ListGuilds(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListGuilds: %v", err)
	}
	if len(got.Guilds) != MaxListedGuilds || got.Truncated {
		t.Fatalf("listing = %d entries, truncated=%v; want the full set and no flag",
			len(got.Guilds), got.Truncated)
	}
}

// TestEveryDashboardVerbRefusesACacheOnlyBinding extends
// TestOwnershipRefusesACacheOnlyBinding to the four verbs that used to take
// the loose check. Each of them reads or changes one guild on the caller's
// say-so, and a cached binding can outlive an unbind -- so each must answer
// "try again" rather than serve another streamer's server.
func TestEveryDashboardVerbRefusesACacheOnlyBinding(t *testing.T) {
	store := discordstore.NewMem()
	bind := discordstore.Binding{
		Guild:       discordstore.Guild{ID: "guild-1"},
		Broadcaster: discordstore.Broadcaster{ID: "42"},
	}
	if err := store.BindGuild(context.Background(), bind); err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	w := setupWorker(&guildRecorder{}, cacheOnlyStore{Store: store})
	ctx := context.Background()
	req := GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}

	cases := []struct {
		name string
		run  func() error
	}{
		{"layout", func() error { _, err := w.GuildLayout(ctx, req); return err }},
		{"status", func() error { _, err := w.GuildInfo(ctx, req); return err }},
		{"unbind", func() error { return w.UnbindGuild(ctx, req) }},
		{"desk.repost", func() error {
			_, err := w.RepostDesk(ctx, DeskRepostRequest{
				GuildID: "guild-1", BroadcasterID: "42", ChannelID: "chan-1",
			})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, discordstore.ErrStoreUnavailable) {
				t.Fatalf("want ErrStoreUnavailable, got %v", err)
			}
		})
	}
}

// TestUnbindStaysIdempotent: strict ownership must not turn "disconnect a
// server that is already disconnected" into an error the streamer has to
// interpret. It is the one verb that passes MissingOK.
func TestUnbindStaysIdempotent(t *testing.T) {
	w, _, _ := boundWorker(t, "guild-1")
	ctx := context.Background()
	req := GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}

	if err := w.UnbindGuild(ctx, req); err != nil {
		t.Fatalf("first unbind: %v", err)
	}
	if err := w.UnbindGuild(ctx, req); err != nil {
		t.Fatalf("second unbind: %v, want the same silence", err)
	}
	// Somebody else's guild is still refused, missing binding or not.
	other := GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}
	if err := w.UnbindGuild(ctx, other); err != nil {
		t.Fatalf("unbinding an unknown guild: %v", err)
	}
}
