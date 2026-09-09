// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

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

// The three {cr.…} families are the three views' own palettes, and they stay
// apart: the ranked and trophy-road spellings of {trophies} are different
// numbers, which is the whole reason for the extra prefix segment.
func TestCrFamiliesMirrorTheirPalettes(t *testing.T) {
	stats := familyByPrefix(t, crTokenPrefix)
	ranked := familyByPrefix(t, crRankedPrefix)
	road := familyByPrefix(t, crRoadTokenPrefix)

	assert.Equal(t, clashroyaleModuleName, stats.Module)
	assert.Equal(t, paletteFields(clashStatsTokens()), stats.Fields)
	assert.Equal(t, paletteFields(clashRankedTokens()), ranked.Fields)
	assert.Equal(t, paletteFields(clashRoadTokens()), road.Fields)
	assert.NotContains(t, stats.Fields, "trophies", "the lifetime profile reports no trophies")
}

func TestCrFamiliesRenderTheCommandsValues(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"clashroyale.stats": clashStatsReply(),
		"clashroyale.ranked": gossiprpc.ClashRoyaleRankedReply{
			Player: "Bagel", Tag: "#P2LQ0GR",
			Current: gossiprpc.ClashRoyaleRankedResult{LeagueNumber: 10, Trophies: 2100, Rank: 321},
			Best:    gossiprpc.ClashRoyaleRankedResult{LeagueNumber: 10, Trophies: 2400, Rank: 42},
		},
		"clashroyale.trophy_road": gossiprpc.ClashRoyaleTrophyRoadReply{
			Player: "Bagel", Tag: "#P2LQ0GR", Trophies: 9123, BestTrophies: 9345,
			Arena: gossiprpc.ClashRoyaleArena{Name: "Legendary Arena"},
		},
	}}
	const linked = `{"account":"#P2LQ0GR"}`

	stats, found := lookOne(t, familyByPrefix(t, crTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "62", stats["level"])
	assert.Equal(t, "Bakery", stats["clan"])

	ranked, found := lookOne(t, familyByPrefix(t, crRankedPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "2100", ranked["trophies"])
	assert.Equal(t, "321", ranked["rank"])

	road, found := lookOne(t, familyByPrefix(t, crRoadTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "9123", road["trophies"], "the trophy road counts different trophies")
	assert.Equal(t, "Legendary Arena", road["arena"])

	assert.Equal(t, "#P2LQ0GR", gw.lastCall(t).req.Account)
}

// The mcsr field lists are spelled out rather than derived (their palettes are
// built per call against the channel's locale), so this is what stops them
// drifting: each list must be exactly the keys its own palette carries.
func TestMcsrFamilyFieldsMatchTheirPalettes(t *testing.T) {
	c := urchinCtx("")

	assert.Equal(t, mcsrEloFields,
		stringPaletteFields(mcsrEloPalette(c, &gossiprpc.McsrUserReply{})))
	assert.Equal(t, mcsrSessionFields,
		stringPaletteFields(mcsrSessionPalette(c, &gossiprpc.McsrSessionReply{})))
	assert.Equal(t, mcsrLastMatchFields,
		stringPaletteFields(mcsrLastMatchPalette(c, &gossiprpc.McsrLastMatchReply{})))
}

func TestMcsrFamiliesRenderTheCommandsValues(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.user": gossiprpc.McsrUserReply{
			Nickname: "Feinberg", Elo: 1650, Rank: 12, Wins: 40, Loses: 20, Played: 65, Country: "us",
		},
		"mcsr.session": gossiprpc.McsrSessionReply{
			Nickname: "Feinberg", Elo: 1650, EloChange: 34, Wins: 4, Loses: 2, Played: 6, HasSnapshot: true,
		},
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{
			Player: "Feinberg", Opponent: "Priffin", Result: "win",
			EloChange: 17, Time: "8:41", Seed: "village", Structure: "buried treasure", AgoSeconds: 900,
		},
	}}
	const linked = `{"account":"Feinberg"}`

	elo, found := lookOne(t, familyByPrefix(t, mcsrTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "1650", elo["elo"])
	assert.Equal(t, "12", elo["rank"])
	assert.Equal(t, "5", elo["draws"], "draws are derived, exactly as !elo derives them")

	session, found := lookOne(t, familyByPrefix(t, mcsrSessionTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "+34", session["elochange"])

	last, found := lookOne(t, familyByPrefix(t, mcsrLastTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "Priffin", last["opponent"])
	assert.Equal(t, "15m", last["ago"])
}

// The session baseline is filed per channel against the linked account, so a
// payload names nobody: the family answers about the streamer either way.
func TestMcsrSessionFamilyIgnoresAPayload(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session": gossiprpc.McsrSessionReply{Nickname: "Feinberg", HasSnapshot: true},
	}}
	_, found := lookOne(t, familyByPrefix(t, mcsrSessionTokenPrefix), gameMount(gw, `{"account":"Feinberg"}`), "Priffin")

	require.True(t, found)
	call := gw.lastCall(t)
	assert.Equal(t, "Feinberg", call.req.Account)
	assert.Equal(t, "2", call.req.ChannelID, "the baseline is this channel's")
}

// A reply carrying nothing renderable is found=false, so every field is empty
// and the fallback speaks: a zero delta would claim the streamer played and
// gained nothing.
func TestMcsrFamiliesReportAnEmptyReplyAsNothing(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"mcsr.session":    gossiprpc.McsrSessionReply{Nickname: "Feinberg", Elo: 1650},
		"mcsr.last_match": gossiprpc.McsrLastMatchReply{Player: "Newbie", Empty: true},
	}}
	const linked = `{"account":"Feinberg"}`

	_, found := lookOne(t, familyByPrefix(t, mcsrSessionTokenPrefix), gameMount(gw, linked), "")
	assert.False(t, found, "no baseline yet")

	_, found = lookOne(t, familyByPrefix(t, mcsrLastTokenPrefix), gameMount(gw, linked), "")
	assert.False(t, found, "never played a match")
}

// MCSR Ranked accepts a stored Mojang uuid and it survives a rename, so the
// families prefer it exactly as the commands do.
func TestMcsrFamilyPrefersTheStoredUuid(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{"mcsr.user": gossiprpc.McsrUserReply{Nickname: "Feinberg"}}}
	_, found := lookOne(t, familyByPrefix(t, mcsrTokenPrefix),
		gameMount(gw, `{"account":"Feinberg","accountUuid":"e4d5aa1c"}`), "")

	require.True(t, found)
	assert.Equal(t, "e4d5aa1c", gw.lastCall(t).req.Account)
}

// The Bed Wars families are !bwstats' and the three period commands' own
// palettes; {urchin.…} is the overlay score, a different question about the
// same player, which is why it keeps the module's own name.
func TestUrchinFamiliesMirrorTheirPalettes(t *testing.T) {
	lifetime := familyByPrefix(t, bwTokenPrefix)
	daily := familyByPrefix(t, bwDailyPrefix)
	sniper := familyByPrefix(t, urchinTokenPrefix)

	assert.Equal(t, urchinModuleName, lifetime.Module)
	assert.Equal(t, paletteFields(urchinStatsTokens()), lifetime.Fields)
	assert.Equal(t, paletteFields(urchinSessionTokens()), daily.Fields)
	assert.Equal(t, paletteFields(urchinSniperTokens()), sniper.Fields)
	assert.Contains(t, lifetime.Fields, "stars", "only the lifetime profile has a star level")
	assert.NotContains(t, daily.Fields, "stars")
}

func TestUrchinFamiliesRenderTheCommandsValues(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"hypixel.stats": gossiprpc.HypixelStatsReply{
			Player: "Techno", Stars: 402, Wins: 1000, Losses: 100,
			FinalKills: 9000, FinalDeaths: 1500, BedsBroken: 4200,
		},
		"urchin.daily": gossiprpc.UrchinSessionReply{
			Player: "Techno", Wins: 5, Losses: 2, FinalKills: 21, FinalDeaths: 3, BedsBroken: 9,
		},
		"urchin.sniper": gossiprpc.UrchinSniperReply{Player: "Techno", Score: 7.5, Mode: "strict", TagCount: 2},
	}}
	const linked = `{"account":"Techno"}`

	lifetime, found := lookOne(t, familyByPrefix(t, bwTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "402", lifetime["stars"])
	assert.Equal(t, "6.00", lifetime["fkdr"])
	assert.Equal(t, "10.00", lifetime["wlr"])

	daily, found := lookOne(t, familyByPrefix(t, bwDailyPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "21", daily["finals"])
	assert.Equal(t, "7.00", daily["fkdr"])

	sniper, found := lookOne(t, familyByPrefix(t, urchinTokenPrefix), gameMount(gw, linked), "")
	require.True(t, found)
	assert.Equal(t, "7.5", sniper["score"])
	assert.Equal(t, "strict", sniper["mode"])
}

// Hypixel REQUIRES a Mojang uuid and Coral accepts one, which is exactly why
// the module stores it beside the name: the families prefer it, as the
// commands do.
func TestUrchinFamiliesPreferTheStoredUuid(t *testing.T) {
	gw := &fakeGossip{replies: map[string]any{
		"hypixel.stats": gossiprpc.HypixelStatsReply{Player: "Techno"},
	}}
	_, found := lookOne(t, familyByPrefix(t, bwTokenPrefix),
		gameMount(gw, `{"account":"Techno","accountUuid":"b876ec32"}`), "")

	require.True(t, found)
	assert.Equal(t, "b876ec32", gw.lastCall(t).req.Account)
}

// Each period family asks its own command's endpoint, so {bw.weekly.finals} and
// !weekly cannot disagree about which window they mean.
func TestBwPeriodFamiliesAskTheirOwnEndpoint(t *testing.T) {
	for prefix, endpoint := range map[string]string{
		bwDailyPrefix: "daily", bwWeeklyPrefix: "weekly", bwMonthlyPrefix: "monthly",
	} {
		gw := &fakeGossip{replies: map[string]any{
			"urchin." + endpoint: gossiprpc.UrchinSessionReply{Player: "Techno"},
		}}
		_, found := lookOne(t, familyByPrefix(t, prefix), gameMount(gw, `{"account":"Techno"}`), "")
		require.True(t, found, prefix)
		assert.Equal(t, endpoint, gw.lastCall(t).endpoint)
	}
}
