// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/scope"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// gameMount binds one family to a channel the way the engine does: the
// broadcaster's config blob, the gossip caller its own commands use.
func gameMount(gw engine.GossipCaller, config string) engine.GameMount {
	return engine.GameMount{
		Ctx:    urchinCtx(""),
		Config: []byte(config),
		Gossip: gw,
		Log:    zap.NewNop(),
	}
}

// familyByPrefix finds one contributed family, so a test names it the way a
// broadcaster does rather than by its index in the list.
func familyByPrefix(t *testing.T, prefix string) engine.GameFamilySpec {
	t.Helper()
	for _, spec := range GameFamilies() {
		if spec.Prefix == prefix {
			return spec
		}
	}
	t.Fatalf("no game-stat family is contributed under %q", prefix)
	return engine.GameFamilySpec{}
}

// lookOne runs one family's lookup against an already-mounted channel and
// returns the palette it renders.
//
// It takes the mount rather than the caller and the config blob separately
// because those two ARE the channel: a test that pairs one broadcaster's config
// with another's gossip is asserting a state the engine cannot produce.
func lookOne(t *testing.T, spec engine.GameFamilySpec, mount engine.GameMount, player string) (scope.Palette, bool) {
	t.Helper()
	palette, found, err := spec.Lookup(mount)(context.Background(), player)
	require.NoError(t, err)
	return palette, found
}

// The families are what the engine gates on, so every one of them must name a
// real opt-in module and a prefix nothing else claims.
func TestGameFamiliesAreDistinctAndGated(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range GameFamilies() {
		assert.NotEmpty(t, spec.Module, "%s must name the module row that gates it", spec.Prefix)
		assert.NotEmpty(t, spec.Fields, "%s must carry the fields it answers", spec.Prefix)
		assert.NotNil(t, spec.Lookup, "%s must carry a lookup", spec.Prefix)
		assert.False(t, seen[spec.Prefix], "two families claim the prefix %q", spec.Prefix)
		seen[spec.Prefix] = true
	}
}

// The {val.…} family is !valrank's own palette: the field list is derived from
// it, so a token cannot go missing when the command grows one.
func TestValFamilyMirrorsTheRankPalette(t *testing.T) {
	spec := familyByPrefix(t, valTokenPrefix)

	assert.Equal(t, valModuleName, spec.Module)
	assert.Equal(t, paletteFields(valRankTokens()), spec.Fields)
	assert.Contains(t, spec.Fields, "tier")
	assert.Contains(t, spec.Fields, "peaktier")
}

// The palette renders the byte-identical values !valrank's template prints.
func TestValFamilyRendersTheCommandsValues(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	palette, found := lookOne(t, familyByPrefix(t, valTokenPrefix), gameMount(gw, `{"account":"Frosty#EUW1"}`), "")

	require.True(t, found)
	assert.Equal(t, scope.Palette{
		"player": "Frosty#EUW1", "region": "eu", "tier": "Immortal 2",
		"elo": "1832", "rr": "67", "lastchange": "-12",
		"peaktier": "Immortal 1", "placement": "513",
	}, palette)
	assert.Equal(t, "Frosty#EUW1", gw.lastCall(t).req.Account)
}

// A payload goes through !val's own argument scoping: a shard word peels off
// wherever it sits, exactly as it does for a typed id.
func TestValFamilyScopesThePayloadLikeTheCommand(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	_, found := lookOne(t, familyByPrefix(t, valTokenPrefix), gameMount(gw, `{"account":"Bagel#EUW"}`), "eu Frosty#EUW1")

	require.True(t, found)
	call := gw.lastCall(t)
	assert.Equal(t, "Frosty#EUW1", call.req.Account)
	assert.Equal(t, "eu", call.req.Region)
}

// The "only my linked account" toggle drops a payload here for the same reason
// it drops a typed id there: it is the module's own resolution, not a copy.
func TestValFamilyHonoursLinkedOnly(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"valorant.rank": valRankReply()}}
	_, found := lookOne(t, familyByPrefix(t, valTokenPrefix),
		gameMount(gw, `{"account":"Bagel#EUW","linkedOnly":"on"}`), "Frosty#EUW1")

	require.True(t, found)
	assert.Equal(t, "Bagel#EUW", gw.lastCall(t).req.Account)
}

// A failed lookup is found=false with the error handed back: the span renders
// empty and the family, not the reply, pays for the outage.
func TestGameFamilyReportsAFailedLookupAsNothing(t *testing.T) {
	gw := &fakeGossip{}
	palette, found, err := familyByPrefix(t, valTokenPrefix).
		Lookup(gameMount(gw, `{"account":"Frosty#EUW1"}`))(context.Background(), "")

	assert.Error(t, err)
	assert.False(t, found)
	assert.Nil(t, palette)
}

// The {fn.…} family is !fnstats's own palette over the all-time window, asked
// for with the platform namespace the linked account lives in.
func TestFnFamilyMirrorsTheStatsPalette(t *testing.T) {
	spec := familyByPrefix(t, fnTokenPrefix)

	assert.Equal(t, fortniteModuleName, spec.Module)
	assert.Equal(t, paletteFields(fortniteStatsTokens()), spec.Fields)
	assert.Contains(t, spec.Fields, "kd")
	assert.Contains(t, spec.Fields, "wins")
}

func TestFnFamilyRendersTheCommandsValues(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	palette, found := lookOne(t, familyByPrefix(t, fnTokenPrefix),
		gameMount(gw, `{"account":"Ninja","accountType":"epic"}`), "")

	require.True(t, found)
	assert.Equal(t, "Ninja", palette["player"])
	assert.Equal(t, "301", palette["wins"])
	assert.Equal(t, "3.66", palette["kd"])
	assert.Equal(t, "4.1", palette["squadkd"])

	call := gw.lastCall(t)
	assert.Equal(t, "Ninja", call.req.Account)
	assert.Equal(t, "epic", call.req.AccountType)
	assert.Equal(t, fortniteLifetimeWindow, call.req.TimeWindow,
		"a token outlives a season, so it reads the all-time window")
}

// Fortnite is name-keyed: a stored uuid must never be substituted for the
// linked name, exactly as !fnstats does not substitute one.
func TestFnFamilyKeepsTheLinkedName(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"fortnite.stats": fortniteStatsReply()}}
	_, found := lookOne(t, familyByPrefix(t, fnTokenPrefix),
		gameMount(gw, `{"account":"Ninja","accountUuid":"0123456789abcdef"}`), "")

	require.True(t, found)
	assert.Equal(t, "Ninja", gw.lastCall(t).req.Account)
}
