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
)

// betaModules always returns a connected, enabled Discord row, so these tests
// isolate the tier gate from every other reason resolution can fail.
type betaModules struct{}

func (betaModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{
		Name: ddiscord.ModuleName, IsEnabled: true,
		Configs: []byte(`{"guildId":"g1"}`),
	}, true, nil
}

// resolverWithTier wires a store holding one bound, configured guild, so a
// gated result is the gate's doing rather than a missing binding.
func resolverWithTier(tier Status) Resolver {
	return Resolver{Modules: betaModules{}, Tier: tier, Store: seededStore(), Log: zap.NewNop()}
}

func seededStore() discordstore.Store {
	mem := discordstore.NewMem()
	_ = mem.BindGuild(context.Background(), discordstore.Binding{Guild: discordstore.Guild{ID: "g1"}, Broadcaster: discordstore.Broadcaster{ID: "1"}})
	mem.PutGuildConfig(discordstore.Guild{ID: "g1"}, ddiscord.Config{GuildID: "g1"})
	return mem
}

func tierOf(status string, known bool) Status {
	return func(context.Context, uint64) (string, bool) { return status, known }
}

func TestPremiumChannelResolves(t *testing.T) {
	for _, status := range []string{"paid", "vip"} {
		if got := resolverWithTier(tierOf(status, true)).ByBroadcaster(context.Background(), 1); len(got) != 1 {
			t.Fatalf("%q channel did not resolve", status)
		}
	}
}

func TestFreeChannelIsGated(t *testing.T) {
	if got := resolverWithTier(tierOf("free", true)).ByBroadcaster(context.Background(), 1); len(got) != 0 {
		t.Fatal("a free channel resolved while Discord is premium-only in beta")
	}
}

// An unreadable tier must close the feature, never open it. This is the
// opposite direction from the identity module, and deliberately so: there a
// wrong guess strips a paying streamer's badge, here it hands out the beta.
func TestUnknownTierIsGated(t *testing.T) {
	if got := resolverWithTier(tierOf("", false)).ByBroadcaster(context.Background(), 1); len(got) != 0 {
		t.Fatal("an unreadable tier resolved; a blip must only ever close the gate")
	}
}

// A service that forgets to wire the tier reader must serve nobody rather
// than everybody.
func TestMissingTierReaderFailsClosed(t *testing.T) {
	r := Resolver{Modules: betaModules{}, Store: seededStore(), Log: zap.NewNop()}
	if got := r.ByBroadcaster(context.Background(), 1); len(got) != 0 {
		t.Fatal("resolver with no tier reader served a channel")
	}
}

// Both directions run the same broadcaster gate, which is why it lives in one
// place: one check covers both input families, and a new module cannot bypass
// it.
func TestGuildDirectionIsGatedToo(t *testing.T) {
	r := resolverWithTier(tierOf("free", true))
	if _, _, ok := r.ByGuild(context.Background(), "g1"); ok {
		t.Fatal("the guild direction bypassed the premium gate")
	}
}

// A premium broadcaster with two servers resolves both: the fan-out is the
// whole point of the slice.
func TestByBroadcasterListsEveryGuild(t *testing.T) {
	r := resolverWithTier(tierOf("paid", true))
	mem := discordstore.NewMem()
	ctx := context.Background()
	for _, id := range []string{"g1", "g2"} {
		_ = mem.BindGuild(ctx, discordstore.Binding{Guild: discordstore.Guild{ID: id}, Broadcaster: discordstore.Broadcaster{ID: "1"}})
		mem.PutGuildConfig(discordstore.Guild{ID: id}, ddiscord.Config{})
	}
	r.Store = mem

	got := r.ByBroadcaster(ctx, 1)
	if len(got) != 2 {
		t.Fatalf("want both guilds, got %d", len(got))
	}
	// configOf stamps the guild id, so a settings row that never carried one
	// still reads as connected.
	if !got[0].Config.Connected() || got[0].Config.GuildID != got[0].Guild.ID {
		t.Fatalf("guild id was not stamped into the config: %+v", got[0])
	}
}

// A bound guild whose settings were never saved resolves to nothing rather
// than to an empty config that every module would then act on.
func TestGuildWithNoSettingsDoesNotResolve(t *testing.T) {
	r := resolverWithTier(tierOf("paid", true))
	mem := discordstore.NewMem()
	_ = mem.BindGuild(context.Background(), discordstore.Binding{Guild: discordstore.Guild{ID: "g9"}, Broadcaster: discordstore.Broadcaster{ID: "1"}})
	r.Store = mem

	if _, _, ok := r.ByGuild(context.Background(), "g9"); ok {
		t.Fatal("a guild with no settings row resolved")
	}
}
