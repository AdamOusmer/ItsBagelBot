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

func resolverWithTier(tier Status) Resolver {
	return Resolver{Modules: betaModules{}, Tier: tier, Log: zap.NewNop()}
}

func tierOf(status string, known bool) Status {
	return func(context.Context, uint64) (string, bool) { return status, known }
}

func TestPremiumChannelResolves(t *testing.T) {
	for _, status := range []string{"paid", "vip"} {
		if _, ok := resolverWithTier(tierOf(status, true)).ByBroadcaster(context.Background(), 1); !ok {
			t.Fatalf("%q channel did not resolve", status)
		}
	}
}

func TestFreeChannelIsGated(t *testing.T) {
	if _, ok := resolverWithTier(tierOf("free", true)).ByBroadcaster(context.Background(), 1); ok {
		t.Fatal("a free channel resolved while Discord is premium-only in beta")
	}
}

// An unreadable tier must close the feature, never open it. This is the
// opposite direction from the identity module, and deliberately so: there a
// wrong guess strips a paying streamer's badge, here it hands out the beta.
func TestUnknownTierIsGated(t *testing.T) {
	if _, ok := resolverWithTier(tierOf("", false)).ByBroadcaster(context.Background(), 1); ok {
		t.Fatal("an unreadable tier resolved; a blip must only ever close the gate")
	}
}

// A service that forgets to wire the tier reader must serve nobody rather
// than everybody.
func TestMissingTierReaderFailsClosed(t *testing.T) {
	r := Resolver{Modules: betaModules{}, Log: zap.NewNop()}
	if _, ok := r.ByBroadcaster(context.Background(), 1); ok {
		t.Fatal("resolver with no tier reader served a channel")
	}
}

// ByGuild routes through ByBroadcaster, which is why the gate lives there:
// one check covers both input families, and a new module cannot bypass it.
func TestGuildDirectionIsGatedToo(t *testing.T) {
	r := resolverWithTier(tierOf("free", true))
	r.Store = discordstore.NewMem()
	_ = r.Store.BindGuild(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Broadcaster{ID: "1"})
	if _, _, ok := r.ByGuild(context.Background(), "g1"); ok {
		t.Fatal("the guild direction bypassed the premium gate")
	}
}
