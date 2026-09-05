// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup

import (
	"context"
	"errors"
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
		if err := store.BindGuild(context.Background(), discordstore.Guild{ID: id}, discordstore.Broadcaster{ID: "42"}); err != nil {
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
	if err != nil || version != 1 {
		t.Fatalf("first save: version %d err %v", version, err)
	}

	cfg, version, found, err := w.GuildConfig(ctx, GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})
	if err != nil || !found || version != 1 || cfg.LiveChannelID != "123" {
		t.Fatalf("read back: %+v v%d found=%v err=%v", cfg, version, found, err)
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
	if len(got) != 2 {
		t.Fatalf("want both servers, got %+v", got)
	}
	for _, g := range got {
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
	if len(got) != 1 {
		t.Fatalf("a kicked bot must not hide the binding, got %+v", got)
	}
	if got[0].BotPresent {
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

	if got := store.GuildsOf(ctx, discordstore.Broadcaster{ID: "42"}); len(got) != 2 {
		t.Fatalf("want two bound guilds, got %+v", got)
	}
}
