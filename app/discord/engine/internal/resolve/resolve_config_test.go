// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package resolve

import (
	"context"
	"testing"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type blobModules struct{}

func (blobModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{Name: ddiscord.ModuleName, IsEnabled: true}, true, nil
}

const testGuildID = "100000000000000001"

func resolverForConfig(t *testing.T, cfg ddiscord.Config, log *zap.Logger) Resolver {
	t.Helper()
	store := discordstore.NewMem()
	if err := store.BindGuild(context.Background(), discordstore.Binding{
		Guild: discordstore.Guild{ID: testGuildID}, Broadcaster: discordstore.Broadcaster{ID: "1"},
	}); err != nil {
		t.Fatalf("BindGuild: %v", err)
	}
	store.PutGuildConfig(discordstore.Guild{ID: testGuildID}, cfg)
	return Resolver{
		Store: store, Modules: blobModules{},
		Tier:   func(context.Context, uint64) (string, bool) { return "paid", true },
		Warned: NewConfigWarnings(), Log: log,
	}
}

func TestResolveZeroesAnInvalidFieldAndKeepsTheRest(t *testing.T) {
	r := resolverForConfig(t, ddiscord.Config{
		LiveChannelID: "100000000000000002", ClipsChannelID: "#clips",
	}, zap.NewNop())

	cfg, _, ok := r.ByGuild(context.Background(), testGuildID)
	if !ok {
		t.Fatal("one bad field must not unresolve the guild")
	}
	if cfg.ClipsChannelID != "" {
		t.Fatalf("clips = %q, want the invalid id dropped", cfg.ClipsChannelID)
	}
	if cfg.LiveChannelID != "100000000000000002" {
		t.Fatalf("live = %q, want it untouched", cfg.LiveChannelID)
	}
}

func TestResolveWarnsOncePerGuildPerField(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	r := resolverForConfig(t, ddiscord.Config{ClipsChannelID: "#clips"}, zap.New(core))

	for range 3 {
		if _, _, ok := r.ByGuild(context.Background(), testGuildID); !ok {
			t.Fatal("guild did not resolve")
		}
	}

	if n := logs.FilterField(zap.String("field", "clipsChannelId")).Len(); n != 1 {
		t.Fatalf("warnings = %d, want 1", n)
	}
}

func TestConfigWarningsAreKeyedPerField(t *testing.T) {
	w := NewConfigWarnings()

	firstSightings := []struct{ guild, field string }{
		{"g1", "clipsChannelId"},
		{"g1", "liveChannelId"},
		{"g2", "clipsChannelId"},
	}
	for _, pair := range firstSightings {
		if !w.first(pair.guild, pair.field) {
			t.Fatalf("%s/%s reported as already warned on its first sighting", pair.guild, pair.field)
		}
	}

	if w.first("g1", "clipsChannelId") {
		t.Fatal("the same pair warned twice")
	}
}

func TestNilConfigWarningsAlwaysWarns(t *testing.T) {
	var w *ConfigWarnings

	if !w.first("g1", "clipsChannelId") || !w.first("g1", "clipsChannelId") {
		t.Fatal("a nil record swallowed a warning")
	}
}
