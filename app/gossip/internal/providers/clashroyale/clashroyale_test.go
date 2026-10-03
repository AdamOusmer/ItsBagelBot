// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package clashroyale_test

import (
	"context"
	"net/http"
	"testing"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/clashroyale"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProvider(t testing.TB, handler http.Handler) provider.Provider {
	return clashroyale.New(
		clashroyale.Config{BaseURL: providertest.Upstream(t, handler), APIKey: "royale-key"},
		providertest.Deps(providertest.NewMemStore()))
}

const playerBody = `{
  "tag":"#P2LQ0GR",
  "name":"Bagel",
  "expLevel":62,
  "expPoints":123456,
  "starPoints":7890,
  "trophies":9123,
  "bestTrophies":9345,
  "wins":600,
  "losses":300,
  "battleCount":1000,
  "threeCrownWins":120,
  "challengeCardsWon":900,
  "challengeMaxWins":12,
  "tournamentCardsWon":500,
  "tournamentBattleCount":40,
  "donations":50,
  "donationsReceived":25,
  "totalDonations":10000,
  "arena":{"id":54000024,"name":"Legendary Arena"},
  "clan":{"tag":"#2Q0","name":"Bakery","badgeId":16000000},
  "currentFavouriteCard":{"id":26000000,"name":"Knight","level":14,"maxLevel":14,"elixirCost":3,"rarity":"common","iconUrls":{"medium":"https://example.test/knight.png"}},
  "currentDeck":[
    {"id":26000000,"name":"Knight","level":14,"maxLevel":14,"elixirCost":3,"rarity":"common","iconUrls":{"medium":"https://example.test/knight.png"}},
    {"id":26000001,"name":"Archers","level":14,"maxLevel":14,"elixirCost":3,"rarity":"common"},
    {"id":26000002,"name":"Goblins","level":14,"maxLevel":14,"elixirCost":2,"rarity":"common"},
    {"id":26000003,"name":"Giant","level":14,"maxLevel":14,"elixirCost":5,"rarity":"rare"},
    {"id":26000004,"name":"P.E.K.K.A","level":14,"maxLevel":14,"elixirCost":7,"rarity":"epic"},
    {"id":26000005,"name":"Minions","level":14,"maxLevel":14,"elixirCost":3,"rarity":"common"},
    {"id":28000000,"name":"Fireball","level":14,"maxLevel":14,"elixirCost":4,"rarity":"rare"},
    {"id":27000000,"name":"Cannon","level":14,"maxLevel":14,"elixirCost":3,"rarity":"common"}
  ],
  "currentDeckSupportCards":[{"id":123,"name":"Tower Troop","level":14,"maxLevel":14,"elixirCost":0,"rarity":"legendary"}],
  "leagueStatistics":{"currentSeason":{"trophies":1900,"bestTrophies":2000}},
  "currentPathOfLegendSeasonResult":{"leagueNumber":10,"trophies":2100,"rank":321},
  "lastPathOfLegendSeasonResult":{"leagueNumber":10,"trophies":2050,"rank":500},
  "bestPathOfLegendSeasonResult":{"leagueNumber":10,"trophies":2400,"rank":42}
}`

func TestEndpointsShareOneNormalizedPlayerFetch(t *testing.T) {
	var hits int
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/players/#P2LQ0GR", r.URL.Path)
		assert.Equal(t, "Bearer royale-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(playerBody))
	}))

	stats := providertest.Call[gossiprpc.ClashRoyaleStatsReply](t, p, "stats", gossiprpc.Request{Account: " #p2lq0gr "})
	require.Empty(t, stats.Error)
	assert.Equal(t, "Bagel", stats.Player)
	assert.Equal(t, "#P2LQ0GR", stats.Tag)
	assert.Equal(t, 62, stats.KingLevel)
	assert.Equal(t, 100, stats.Draws)
	assert.InDelta(t, 60, stats.WinRate, 1e-9)
	assert.Equal(t, "Bakery", stats.Clan.Name)
	assert.Equal(t, "Knight", stats.FavouriteCard.Name)

	decks := providertest.Call[gossiprpc.ClashRoyaleDecksReply](t, p, "decks", gossiprpc.Request{Account: "P2LQ0GR"})
	require.Empty(t, decks.Error)
	require.Len(t, decks.CurrentDeck, 8)
	assert.Equal(t, "Knight", decks.CurrentDeck[0].Name)
	assert.Equal(t, "https://example.test/knight.png", decks.CurrentDeck[0].IconURLs.Medium)
	assert.Len(t, decks.SupportCards, 1)
	assert.InDelta(t, 3.75, decks.AverageElixir, 1e-9)

	ranked := providertest.Call[gossiprpc.ClashRoyaleRankedReply](t, p, "ranked", gossiprpc.Request{Account: "#P2LQ0GR"})
	require.Empty(t, ranked.Error)
	assert.False(t, ranked.Unranked)
	assert.Equal(t, 10, ranked.Current.LeagueNumber)
	assert.Equal(t, 2100, ranked.Current.Trophies)
	assert.Equal(t, 321, ranked.Current.Rank)
	assert.Equal(t, 42, ranked.Best.Rank)

	road := providertest.Call[gossiprpc.ClashRoyaleTrophyRoadReply](t, p, "trophy_road", gossiprpc.Request{Account: "P2LQ0GR"})
	require.Empty(t, road.Error)
	assert.Equal(t, 9123, road.Trophies)
	assert.Equal(t, 9345, road.BestTrophies)
	assert.Equal(t, "Legendary Arena", road.Arena.Name)

	assert.Equal(t, 1, hits, "all endpoint views must share the profile cache")
}

func TestRankedFallsBackToLeagueStatistics(t *testing.T) {
	p := newProvider(t, providertest.Respond(http.StatusOK, `{
		"tag":"#P2LQ0GR","name":"Legacy",
		"leagueStatistics":{
			"currentSeason":{"id":"2026-07","trophies":1800,"bestTrophies":1900},
			"previousSeason":{"id":"2026-06","trophies":1700,"rank":900},
			"bestSeason":{"id":"2026-05","trophies":2200,"rank":100}
		}
	}`))

	reply := providertest.Call[gossiprpc.ClashRoyaleRankedReply](t, p, "ranked", gossiprpc.Request{Account: "P2LQ0GR"})
	require.Empty(t, reply.Error)
	assert.False(t, reply.Unranked)
	assert.Equal(t, "2026-07", reply.Current.SeasonID)
	assert.Equal(t, 1900, reply.Current.BestTrophies)
	assert.Equal(t, "2026-06", reply.Previous.SeasonID)
	assert.Equal(t, "2026-05", reply.Best.SeasonID)
}

func TestPlayerTagsAreNormalizedOrRejectedBeforeAnyUpstreamCall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		endpoint  string
		account   string
		wantError string
		wantPath  string
	}{
		{"rejects a missing account", "stats", "", "missing account", ""},
		{"rejects characters outside the tag alphabet", "decks", "#ABC123", "invalid player tag", ""},
		{"rejects a tag that is too short", "ranked", "P2", "invalid player tag", ""},
		{"rejects a tag that is too long", "trophy_road", "P2LQ0GRP2LQ0GRP2", "invalid player tag", ""},
		{"rejects latin-1 letters without panicking", "stats", "ÿÿÿ", "invalid player tag", ""},
		{"rejects a non-ASCII rune inside a tag", "stats", "2ÿ9", "invalid player tag", ""},
		{"rejects a non-ASCII rune behind the hash", "stats", "#2ÿ9", "invalid player tag", ""},
		{"rejects accented letters", "stats", "ñññ", "invalid player tag", ""},
		{"rejects full-width digits", "stats", "２８９", "invalid player tag", ""},
		{"normalizes case, spaces, and the letter O to zero", "stats", " #p2lqogr ", "", "/players/#P2LQ0GR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(playerBody))
			}))

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), gossiprpc.Request{Account: tc.account})
			assert.Equal(t, tc.wantError, providertest.ErrorOf(t, res))
			assert.Equal(t, tc.wantPath, gotPath)
		})
	}
}

func TestNotFoundIsFriendlyAndNegativeCachedAcrossEndpoints(t *testing.T) {
	var hits int
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"reason":"notFound"}`))
	}))

	stats := providertest.Call[gossiprpc.ClashRoyaleStatsReply](t, p, "stats", gossiprpc.Request{Account: "P2LQ0GR"})
	assert.Equal(t, "player not found", stats.Error)
	road := providertest.Call[gossiprpc.ClashRoyaleTrophyRoadReply](t, p, "trophy_road", gossiprpc.Request{Account: "P2LQ0GR"})
	assert.Equal(t, "player not found", road.Error)
	assert.Equal(t, 1, hits)
}

func TestUpstreamProfilesAreNormalizedForChat(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want gossiprpc.ClashRoyaleStatsReply
	}{
		{"reports a profile without a tag as an unknown player", `{"name":"Ghost"}`,
			gossiprpc.ClashRoyaleStatsReply{Error: "player not found", Tag: "#P2LQ0GR"}},
		{"never reports negative draws", `{"tag":"#P2LQ0GR","name":"Odd","wins":8,"losses":5,"battleCount":10}`,
			gossiprpc.ClashRoyaleStatsReply{Player: "Odd", Tag: "#P2LQ0GR", Wins: 8, Losses: 5, Battles: 10, WinRate: 80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, providertest.Respond(http.StatusOK, tc.body))
			assert.Equal(t, tc.want, providertest.Call[gossiprpc.ClashRoyaleStatsReply](t, p, "stats", gossiprpc.Request{Account: "P2LQ0GR"}))
		})
	}
}

func TestProviderDeclaresItsRPCEndpoints(t *testing.T) {
	p := clashroyale.New(clashroyale.Config{APIKey: "key"}, providertest.Deps(providertest.NewMemStore()))

	var names []string
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Equal(t, "clashroyale", p.Name())
	assert.Equal(t, []string{"stats", "decks", "ranked", "trophy_road"}, names)
}
