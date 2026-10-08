// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package resolve_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/resolve"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const validChannel = "100000000000000002"

type fakeModules struct {
	disabled bool
	err      error
}

func (m fakeModules) GetModule(context.Context, uint64, string) (projection.ModuleView, bool, error) {
	return projection.ModuleView{Name: ddiscord.ModuleName, IsEnabled: !m.disabled}, true, m.err
}

func tierOf(status string, known bool) resolve.Status {
	return func(context.Context, uint64) (string, bool) { return status, known }
}

type rig struct {
	configs map[string]ddiscord.Config
	bare    []string
	modules fakeModules
	tier    resolve.Status
	warned  *resolve.ConfigWarnings
	log     *zap.Logger
}

func (g rig) resolver(t *testing.T) resolve.Resolver {
	t.Helper()
	store := discordstore.NewMem()
	bind := func(id string) discordstore.Guild {
		guild := discordstore.Guild{ID: id}
		require.NoError(t, store.BindGuild(context.Background(), discordstore.Binding{Guild: guild, Broadcaster: discordstore.Broadcaster{ID: "1"}}))
		return guild
	}
	for id, cfg := range g.configs {
		store.PutGuildConfig(bind(id), cfg)
	}
	for _, id := range g.bare {
		bind(id)
	}
	return resolve.Resolver{Store: store, Modules: g.modules, Tier: g.tier, Warned: g.warned, Log: g.log}
}

func oneGuild(tier resolve.Status) rig {
	return rig{configs: map[string]ddiscord.Config{"g1": {}}, tier: tier, log: zap.NewNop()}
}

func TestPremiumGate(t *testing.T) {
	cases := []struct {
		name string
		rig  rig
		open bool
	}{
		{"a paid channel resolves", oneGuild(tierOf("paid", true)), true},
		{"a vip channel resolves", oneGuild(tierOf("vip", true)), true},
		{"a free channel is gated while Discord is premium-only in beta", oneGuild(tierOf("free", true)), false},
		{"an unreadable tier is gated, a blip only ever closes the gate", oneGuild(tierOf("", false)), false},
		{"a missing tier reader fails closed", oneGuild(nil), false},
		{"a disabled module is gated", rig{configs: map[string]ddiscord.Config{"g1": {}}, modules: fakeModules{disabled: true}, tier: tierOf("paid", true)}, false},
		{"a module read failure is gated", rig{configs: map[string]ddiscord.Config{"g1": {}}, modules: fakeModules{err: errors.New("projection down")}, tier: tierOf("paid", true)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.rig.resolver(t)

			byBroadcaster := r.ByBroadcaster(context.Background(), 1)
			_, _, byGuild := r.ByGuild(context.Background(), "g1")

			require.Equal(t, tc.open, len(byBroadcaster) == 1, "broadcaster direction")
			require.Equal(t, tc.open, byGuild, "guild direction")
		})
	}
}

func TestResolvesOnlyGuildsWithSettings(t *testing.T) {
	r := rig{
		configs: map[string]ddiscord.Config{"g1": {}, "g2": {}},
		bare:    []string{"g9"},
		tier:    tierOf("paid", true),
	}.resolver(t)
	ctx := context.Background()

	got := r.ByBroadcaster(ctx, 1)
	_, broadcaster, resolved := r.ByGuild(ctx, "g1")
	_, _, unconfigured := r.ByGuild(ctx, "g9")

	require.Len(t, got, 2, "a guild with no settings row is not listed")
	for _, g := range got {
		require.Equal(t, g.Guild.ID, g.Config.GuildID, "the guild id is stamped into its config")
	}
	require.True(t, resolved)
	require.Equal(t, "1", broadcaster)
	require.False(t, unconfigured)
}

func TestInvalidConfigFieldIsDroppedAndTheRestKept(t *testing.T) {
	r := rig{
		configs: map[string]ddiscord.Config{"g1": {LiveChannelID: validChannel, ClipsChannelID: "#clips"}},
		tier:    tierOf("paid", true),
	}.resolver(t)

	cfg, _, ok := r.ByGuild(context.Background(), "g1")

	require.True(t, ok, "one bad field must not unresolve the guild")
	require.Equal(t, ddiscord.Config{GuildID: "g1", LiveChannelID: validChannel}, cfg)
}

func TestInvalidConfigFieldsWarnOncePerGuildPerField(t *testing.T) {
	bad := ddiscord.Config{ClipsChannelID: "#clips", LiveChannelID: "#live"}
	cases := []struct {
		name   string
		warned *resolve.ConfigWarnings
		want   []string
	}{
		{"a shared record warns once per pair", resolve.NewConfigWarnings(),
			[]string{"g1/clipsChannelId", "g1/liveChannelId", "g2/clipsChannelId", "g2/liveChannelId"}},
		{"a nil record never swallows a warning", nil,
			[]string{
				"g1/clipsChannelId", "g1/clipsChannelId", "g1/clipsChannelId", "g1/liveChannelId", "g1/liveChannelId", "g1/liveChannelId",
				"g2/clipsChannelId", "g2/clipsChannelId", "g2/clipsChannelId", "g2/liveChannelId", "g2/liveChannelId", "g2/liveChannelId",
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			r := rig{
				configs: map[string]ddiscord.Config{"g1": bad, "g2": bad},
				tier:    tierOf("paid", true), warned: tc.warned, log: zap.New(core),
			}.resolver(t)

			for range 3 {
				for _, guild := range []string{"g1", "g2"} {
					_, _, ok := r.ByGuild(context.Background(), guild)
					require.True(t, ok)
				}
			}

			var got []string
			for _, entry := range logs.All() {
				fields := entry.ContextMap()
				got = append(got, fields["guild_id"].(string)+"/"+fields["field"].(string))
			}
			require.ElementsMatch(t, tc.want, got)
		})
	}
}

func TestDroppedGuildsWarnOncePerReason(t *testing.T) {
	cases := []struct {
		name  string
		rig   rig
		guild string
	}{
		{"an unbound guild", oneGuild(tierOf("paid", true)), "stranger"},
		{"a disabled module", rig{configs: map[string]ddiscord.Config{"g1": {}}, modules: fakeModules{disabled: true}, tier: tierOf("paid", true)}, "g1"},
		{"a closed premium gate", oneGuild(tierOf("free", true)), "g1"},
		{"a bound guild without settings", rig{bare: []string{"g1"}, tier: tierOf("paid", true)}, "g1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			tc.rig.warned, tc.rig.log = resolve.NewConfigWarnings(), zap.New(core)
			r := tc.rig.resolver(t)

			for range 3 {
				_, _, ok := r.ByGuild(context.Background(), tc.guild)
				require.False(t, ok)
			}

			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.guild, entries[0].ContextMap()["guild_id"])
		})
	}
}

func TestDirectMessagesAreDroppedSilently(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	r := rig{warned: resolve.NewConfigWarnings(), log: zap.New(core), tier: tierOf("paid", true)}.resolver(t)

	_, _, ok := r.ByGuild(context.Background(), "")

	require.False(t, ok)
	require.Zero(t, logs.Len())
}
