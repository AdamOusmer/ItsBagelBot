// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// The engine cannot see a real palette — the modules package owns every one of
// them and imports this one — so the mount path is asserted against a stand-in
// family shaped exactly like the ones the modules contribute: a prefix, a field
// list, and a lookup that reads the broadcaster's config blob for the linked
// account and asks gossip for the rest.

const (
	testGameModule = "valorant"
	testGamePrefix = "val."
)

var testGameFields = []string{"player", "tier"}

// linkedOn is the module row of a broadcaster who linked an account; the
// stand-in family reads it the way a real one decodes its own config.
func linkedOn(account string) projection.ModuleView {
	return projection.ModuleView{IsEnabled: true, Configs: []byte(`{"account":"` + account + `"}`)}
}

// gameSpy records what the family was mounted with and asked for.
type gameSpy struct {
	config  string
	players []string
}

func (g *gameSpy) spec() GameFamilySpec {
	return GameFamilySpec{
		Prefix: testGamePrefix,
		Module: testGameModule,
		Fields: testGameFields,
		Lookup: g.lookup,
	}
}

func (g *gameSpy) lookup(m GameMount) scope.GameLookup {
	g.config = string(m.Config)
	linked := linkedAccountOf(g.config)
	return func(_ context.Context, player string) (scope.Palette, bool, error) {
		if player == "" {
			player = linked
		}
		g.players = append(g.players, player)
		if player == "" {
			return nil, false, nil
		}
		return scope.Palette{"player": player, "tier": "Ascendant 2"}, true, nil
	}
}

// linkedAccountOf is the two-line stand-in for a module's own config decode.
func linkedAccountOf(config string) string {
	_, rest, found := strings.Cut(config, `"account":"`)
	if !found {
		return ""
	}
	account, _, _ := strings.Cut(rest, `"`)
	return account
}

// gameFixture is one channel's wiring for the game-stat tokens: the response,
// which module rows are on, and which families are contributed.
type gameFixture struct {
	response string
	modules  map[string]projection.ModuleView
	families []GameFamilySpec
	noGossip bool
}

func gamePipeline(t *testing.T, f gameFixture) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: f.response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  f.modules,
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Games:    f.families,
		Log:      zap.NewNop(),
	}
	if !f.noGossip {
		d.Gossip = &stubGossip{}
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

// TestGameTokensExpandThroughTheModuleGate is the table: one template, one set
// of module rows, and the line chat sees.
func TestGameTokensExpandThroughTheModuleGate(t *testing.T) {
	cases := []struct {
		name    string
		fixture gameFixture
		want    string
	}{
		{
			name: "an enabled module expands the family from the linked account",
			fixture: gameFixture{
				response: "{val.player} is {val.tier}",
				modules:  map[string]projection.ModuleView{testGameModule: linkedOn("Bagel#EUW")},
			},
			want: "Bagel#EUW is Ascendant 2",
		},
		{
			name: "a payload targets another player",
			fixture: gameFixture{
				response: "{val.tier:Frosty#EUW1} for {val.player:Frosty#EUW1}",
				modules:  map[string]projection.ModuleView{testGameModule: linkedOn("Bagel#EUW")},
			},
			want: "Ascendant 2 for Frosty#EUW1",
		},
		{
			name: "the module off leaves every spelling literal",
			fixture: gameFixture{
				response: "{val.player} {val.tier}",
				modules:  map[string]projection.ModuleView{testGameModule: off()},
			},
			want: "{val.player} {val.tier}",
		},
		{
			name: "a channel that never enabled the module leaves the span literal",
			fixture: gameFixture{
				response: "{val.tier}",
			},
			want: "{val.tier}",
		},
		{
			name: "an enabled module with nothing linked renders the fallback",
			fixture: gameFixture{
				response: "{val.tier|no rank yet}",
				modules:  map[string]projection.ModuleView{testGameModule: on()},
			},
			want: "no rank yet",
		},
		{
			name: "an unknown field under a known prefix stays literal",
			fixture: gameFixture{
				response: "{val.teir} {val.tier}",
				modules:  map[string]projection.ModuleView{testGameModule: linkedOn("Bagel#EUW")},
			},
			want: "{val.teir} Ascendant 2",
		},
		{
			name: "no gossip wired leaves the span literal",
			fixture: gameFixture{
				response: "{val.tier}",
				modules:  map[string]projection.ModuleView{testGameModule: linkedOn("Bagel#EUW")},
				noGossip: true,
			},
			want: "{val.tier}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &gameSpy{}
			f := tc.fixture
			if f.families == nil {
				f.families = []GameFamilySpec{spy.spec()}
			}
			assert.Equal(t, tc.want, expandViewer(t, gamePipeline(t, f), "!brag"))
		})
	}
}

// TestGameTokensSpendOncePerPlayer is the table for what a run COSTS, where the
// table above is for what chat sees: which config blob the family was mounted
// with, and which players it actually looked up.
//
// The two stay separate tables because they fail for different reasons — a
// rendering bug moves the first, a mounting or batching bug moves the second —
// and one table asserting both would report either failure as the other.
func TestGameTokensSpendOncePerPlayer(t *testing.T) {
	const linkedConfig = `{"account":"Bagel#EUW"}`
	cases := []struct {
		name     string
		response string
		want     string
		// config and players are the zero values for a family that never
		// mounted, so a case that expects no spend simply leaves them out.
		config  string
		players []string
	}{
		{
			// The family is handed the broadcaster's own config blob, which is
			// where its linked account comes from: the token and the module's
			// chat command therefore answer about the same player.
			name:     "the family mounts with the broadcaster's own config",
			response: "{val.player}",
			want:     "Bagel#EUW",
			config:   linkedConfig,
			players:  []string{"Bagel#EUW"},
		},
		{
			// Several fields of one family are one lookup per PLAYER, not one
			// per span; a span naming somebody else is one more.
			name:     "several fields of one family cost one lookup per player",
			response: "{val.player} {val.tier} {val.tier:Frosty#EUW1}",
			want:     "Bagel#EUW Ascendant 2 Ascendant 2",
			config:   linkedConfig,
			players:  []string{"Bagel#EUW", "Frosty#EUW1"},
		},
		{
			// A response naming no game token never mounts a family, so it
			// costs no projection read and no upstream call — the reason the
			// specs carry their prefix and fields statically.
			name:     "a response naming no game token never mounts the family",
			response: "hello {user}",
			want:     "hello alice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &gameSpy{}
			p := gamePipeline(t, gameFixture{
				response: tc.response,
				modules:  map[string]projection.ModuleView{testGameModule: linkedOn("Bagel#EUW")},
				families: []GameFamilySpec{spy.spec()},
			})

			assert.Equal(t, tc.want, expandViewer(t, p, "!brag"))
			assert.Equal(t, tc.config, spy.config, "the config the family mounted with")
			assert.Equal(t, tc.players, spy.players, "the players it looked up")
		})
	}
}
