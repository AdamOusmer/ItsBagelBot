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

// blobModules serves one fixed module row, so these tests isolate what
// resolve does to a stored config from every other reason resolution can
// fail. The row now carries only the master switch: the settings themselves
// come from the store, per guild.
type blobModules struct{}

func (blobModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{Name: ddiscord.ModuleName, IsEnabled: true}, true, nil
}

// testGuildID is the guild every case here resolves; a real snowflake,
// because SanitizeConfig refuses anything that cannot be one.
const testGuildID = "100000000000000001"

// resolverForConfig binds testGuildID to broadcaster 1 and stores cfg as that
// guild's settings.
//
// Merge note (2026-09-05): these cases used to hand resolve a raw module
// blob, which is where the settings lived. They moved to discord-data, so
// the fixture is a store rather than a blob -- the behaviour under test
// (one bad field is dropped, warned about once, and does not take the guild
// down with it) is unchanged.
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

// A stored config can hold a value no dashboard would send today (an older
// console, a hand edit, a rule added after the write). The bad field is
// dropped and the rest of the guild keeps working -- refusing the whole
// config would take that channel's alerts down instead.
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

// The warning fires once per guild per field. Without the dedupe it fires
// on EVERY event for that guild -- thousands of identical lines an hour,
// which is how a real signal gets filtered out and then ignored.
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

// A second bad field is new information and must warn on its own, or the
// first one silences every later mistake in that guild.
func TestConfigWarningsAreKeyedPerField(t *testing.T) {
	w := NewConfigWarnings()

	if !w.first("g1", "clipsChannelId") || !w.first("g1", "liveChannelId") || !w.first("g2", "clipsChannelId") {
		t.Fatal("a first sighting of a pair reported as already warned")
	}
	if w.first("g1", "clipsChannelId") {
		t.Fatal("the same pair warned twice")
	}
}

// A nil record is what a caller that never wired one has (tests, and any
// future service). It must stay noisy rather than silent: a dropped field
// nobody is told about is the failure this whole path exists to surface.
func TestNilConfigWarningsAlwaysWarns(t *testing.T) {
	var w *ConfigWarnings

	if !w.first("g1", "clipsChannelId") || !w.first("g1", "clipsChannelId") {
		t.Fatal("a nil record swallowed a warning")
	}
}
