// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	riotrpc "ItsBagelBot/internal/domain/rpc/riot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	linkedChannel   = "1001"
	unlinkedChannel = "2002"
)

type fakeLinks map[string]riotrpc.Link

func (f fakeLinks) RiotLink(_ context.Context, broadcasterID string) (riotrpc.LinkReply, error) {
	link, ok := f[broadcasterID]
	return riotrpc.LinkReply{Linked: ok, Link: link}, nil
}

func testLinks() fakeLinks {
	return fakeLinks{linkedChannel: {Puuid: "puuid-1", GameName: "Frosty", TagLine: "EUW1", Shard: "eu"}}
}

func linkedDeps(store *providertest.MemStore) provider.Deps {
	d := providertest.Deps(store)
	d.RiotLinks = testLinks()
	return d
}

func newProviderWithStore(t testing.TB, henrik, content http.Handler, store *providertest.MemStore) provider.Provider {
	return New(Config{
		BaseURL:        providertest.Upstream(t, henrik),
		ContentBaseURL: providertest.Upstream(t, content),
		APIKey:         "val-key",
	}, linkedDeps(store))
}

func frosty(region, platform string) gossiprpc.Request {
	return gossiprpc.Request{ChannelID: linkedChannel, Region: region, Platform: platform}
}

func newTestProvider(t testing.TB, henrik, content http.Handler) provider.Provider {
	return newProviderWithStore(t, henrik, content, providertest.NewMemStore())
}

type hitCounter struct {
	mu   sync.Mutex
	hits map[string]int
}

func (c *hitCounter) count(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hits == nil {
		c.hits = map[string]int{}
	}
	c.hits[key]++
}

func (c *hitCounter) get(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[key]
}

const mmrBody = `{
  "status":200,
  "data":{
    "account":{"puuid":"puuid-1","name":"Frosty","tag":"EUW1"},
    "current":{
      "elo":1849,"rr":63,"last_change":-12,
      "tier":{"id":23,"name":"Immortal 1"},
      "leaderboard_placement":{"rank":812,"updated_at":"2026-08-22T00:00:00Z"}
    },
    "peak":{
      "season":{"id":"ab57","short":"S25"},
      "ranking_schema":"Competitive",
      "tier":{"id":21,"name":"Ascendant 2"},
      "rr":80
    },
    "seasonal":[
      {"season":{"id":"22d1","short":"e8a2"},"wins":22,"games":42,"end_tier":{"id":18,"name":"Diamond 1"},
       "ranking_schema":"ascendant","leaderboard_placement":null,"act_wins":[{"id":18,"name":"Diamond 1"}]},
      {"season":{"id":"4539","short":"e8a3"},"wins":0,"games":0,"end_tier":{"id":0,"name":"Unrated"},
       "ranking_schema":"ascendant","leaderboard_placement":null,"act_wins":[]},
      {"season":{"id":"ab57","short":"e9a1"},"wins":51,"games":100,"end_tier":{"id":21,"name":"Ascendant 2"},
       "ranking_schema":"ascendant","leaderboard_placement":{"rank":812,"updated_at":"2026-08-22T00:00:00Z"},
       "act_wins":[{"id":21,"name":"Ascendant 2"}]}
    ]
  }
}`

const accountBody = `{
  "status":200,
  "data":{
    "puuid":"puuid-1","region":"eu","name":"Frosty","tag":"EUW1",
    "account_level":231,
    "card":"https://media.test/card.png",
    "title":"Vanquisher",
    "updated_at":"2026-08-20T00:00:00Z",
    "platforms":["pc"]
  }
}`

const matchesBody = `{
  "status":200,
  "data":[
    {
      "metadata":{"map":{"id":"7eaecc1b","name":"Ascent"},"started_at":"%s","is_completed":true},
      "players":[
        {"puuid":"p-self","name":"Frosty","tag":"EUW1","team_id":"Red","agent":{"name":"Jett"},
         "stats":{"kills":24,"deaths":15,"assists":7,"score":4563}},
        {"puuid":"p-other","name":"Rival","tag":"2222","team_id":"Blue","agent":{"name":"Brimstone"},
         "stats":{"kills":10,"deaths":20,"assists":3,"score":2100}}
      ],
      "teams":[
        {"team_id":"Red","won":true,"rounds":{"won":14,"lost":10}},
        {"team_id":"Blue","won":false,"rounds":{"won":10,"lost":14}}
      ]
    },
    {
      "metadata":{"map":{"id":"e219598c","name":"Bind"},"started_at":"%s","is_completed":false},
      "players":[
        {"puuid":"p-self","name":"Frosty","tag":"EUW1","team_id":"Blue","agent":{"name":"Omen"},
         "stats":{"kills":5,"deaths":2,"assists":1,"score":800}}
      ],
      "teams":[{"team_id":"Blue","won":false,"rounds":{"won":3,"lost":4}}]
    },
    {
      "metadata":{"map":{"id":"2bee0cdc","name":"Pearl"},"started_at":"%s","is_completed":true},
      "players":[
        {"puuid":"p-self","name":"Frosty","tag":"EUW1","team_id":"Blue","agent":{"name":"Sova"},
         "stats":{"kills":11,"deaths":17,"assists":4,"score":2412}}
      ],
      "teams":[
        {"team_id":"Blue","won":false,"rounds":{"won":6,"lost":13}},
        {"team_id":"Red","won":true,"rounds":{"won":13,"lost":6}}
      ]
    }
  ]
}`

const leaderboardBody = `{
  "status":200,
  "data":{
    "players":[
      {"leaderboard_rank":5,"name":"Five","tag":"5555","wins":30,"rr":700,"tier":25,"is_anonymized":false},
      {"leaderboard_rank":1,"name":"One","tag":"1111","wins":42,"rr":910,"tier":27,"is_anonymized":false},
      {"leaderboard_rank":11,"name":"Eleven","tag":"eeee","wins":9,"rr":420,"tier":24,"is_anonymized":false},
      {"leaderboard_rank":3,"name":"Three","tag":"3333","wins":33,"rr":750,"tier":26,"is_anonymized":true}
    ]
  }
}`

func henrik(t testing.TB, routes map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for prefix, body := range routes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				fmt.Fprint(w, body)
				return
			}
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestRankFetchesMMRWithPlainAuthorizationHeader(t *testing.T) {
	var gotAuth string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		assert.Equal(t, "/valorant/v3/mmr/na/pc/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, mmrBody)
	}), providertest.Forbid(t))

	reply := providertest.Call[rankReply](t, p, "rank", frosty("NA", ""))

	assert.Equal(t, "val-key", gotAuth)
	assert.Equal(t, rankReply{
		Player: "Frosty#EUW1", Region: "na", Tier: "Immortal 1", Elo: 1849, RR: 63,
		LastChange: -12, PeakTier: "Ascendant 2", Placement: 812,
	}, reply, "the linked Riot ID routes the lookup, region normalizes, the single peak object reads directly")
}

func TestUnrankedAccountsHideTheirEloNoise(t *testing.T) {
	p := newTestProvider(t, providertest.Respond(http.StatusOK,
		`{"status":200,"data":{"current":{"elo":0,"rr":0,"last_change":0,"tier":{"id":0,"name":"UNRANKED"},"leaderboard_placement":{"rank":0}},"peak":{"tier":{"id":0,"name":"UNRANKED"},"rr":0}}}`),
		providertest.Forbid(t))

	reply := providertest.Call[rankReply](t, p, "rank", frosty("eu", ""))

	assert.Equal(t, rankReply{Player: "Frosty#EUW1", Region: "eu", Tier: "UNRANKED", Unranked: true}, reply,
		"elo of an unranked account is noise; templates should never see it")
}

func TestRecordTotalsRankedGamesAcrossEveryAct(t *testing.T) {
	const unplayed = `{"status":200,"data":{"current":{"tier":{"id":0,"name":"UNRANKED"}},"seasonal":[]}}`
	for _, tc := range []struct {
		name string
		body string
		want recordReply
	}{
		{"played acts are summed and unplayed ones add nothing", mmrBody,
			recordReply{Player: "Frosty#EUW1", Region: "na", Games: 142, Wins: 73}},
		{"an account with no ranked games is an answer, not an error", unplayed,
			recordReply{Player: "Frosty#EUW1", Region: "na", Empty: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/valorant/v3/mmr/na/pc/Frosty/EUW1", r.URL.Path)
				fmt.Fprint(w, tc.body)
			}), providertest.Forbid(t))

			reply := providertest.Call[recordReply](t, p, "record", frosty("NA", ""))

			assert.Equal(t, tc.want, reply)
		})
	}
}

func TestRequestsNeedALinkedChannelBeforeAnyUpstreamCall(t *testing.T) {
	const badRegion = "unknown region (want na, eu, ap, kr, br or latam)"
	for _, tc := range []struct {
		name      string
		endpoint  string
		req       gossiprpc.Request
		wantError string
	}{
		{"no channel", "rank", gossiprpc.Request{}, "missing channel"},
		{"a typed Riot ID is not a link", "matches", gossiprpc.Request{Account: "Frosty#EUW1"}, "missing channel"},
		{"a channel that never signed in", "rank", gossiprpc.Request{ChannelID: unlinkedChannel}, notLinkedMessage},
		{"an unlinked channel's account", "account", gossiprpc.Request{ChannelID: unlinkedChannel}, notLinkedMessage},
		{"a region outside the affinities", "rank", frosty("es", ""), badRegion},
		{"a shard name instead of an affinity", "rank", frosty("euw", ""), badRegion},
		{"an unknown platform", "rank", frosty("", "mobile"), "unknown platform (want pc or console)"},
		{"a leaderboard without a region or a channel", "leaderboard", gossiprpc.Request{}, "missing region"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, providertest.Forbid(t), providertest.Forbid(t))

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), tc.req)

			assert.Contains(t, providertest.ErrorOf(t, res), tc.wantError)
		})
	}
}

func TestRankCachesAbsenceForTheWindow(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{
		Status: http.StatusNotFound,
		Body:   `{"status":404,"errors":[{"code":"NO_ACCOUNT","message":"No account found"}]}`,
	})
	p := newTestProvider(t, upstream, providertest.Forbid(t))

	first := providertest.Call[rankReply](t, p, "rank", frosty("ap", ""))
	second := providertest.Call[rankReply](t, p, "rank", frosty("ap", ""))

	assert.Equal(t, rankReply{Error: "player not found"}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, upstream.Hits(), "404s are negatively cached for the window")
}

func TestAutoRegionComesFromTheLinkedShard(t *testing.T) {
	var counter hitCounter
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.count(r.URL.Path)
		fmt.Fprint(w, mmrBody)
	}), providertest.Forbid(t))

	for range 3 {
		reply := providertest.Call[rankReply](t, p, "rank", frosty("", ""))
		assert.Equal(t, "eu", reply.Region)
	}

	assert.Equal(t, 1, counter.get("/valorant/v3/mmr/eu/pc/Frosty/EUW1"),
		"the shard saved at sign-in routes the lookup, and rank replies collapse onto one cached flight")
}

func TestMatchesSummarizesCompletedGamesOnly(t *testing.T) {
	anHourAgo := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(matchesBody, anHourAgo, anHourAgo, anHourAgo)
	var gotQuery string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		assert.Equal(t, "/valorant/v4/matches/na/pc/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, body)
	}), providertest.Forbid(t))

	reply := providertest.Call[matchesReply](t, p, "matches", frosty("na", ""))

	assert.Equal(t, "mode=competitive&size=5", gotQuery,
		"the competitive-only filter rides the upstream query, not client-side guessing")
	require.Len(t, reply.Matches, 2, "an incomplete game is skipped, not shown as a ghost row")
	assert.GreaterOrEqual(t, reply.Matches[0].AgoSeconds, int64(3590))
	for i := range reply.Matches {
		reply.Matches[i].AgoSeconds = 0
	}
	assert.Equal(t, []matchEntry{
		{Map: "Ascent", Agent: "Jett", Result: "win", Kills: 24, Deaths: 15, Assists: 7, ACS: 190.1},
		{Map: "Pearl", Agent: "Sova", Result: "loss", Kills: 11, Deaths: 17, Assists: 4, ACS: 126.9},
	}, reply.Matches, "ACS is score over rounds played, rounded to one decimal")
}

func TestMatchesEmptyHistoryIsAnAnswerNotAnError(t *testing.T) {
	p := newTestProvider(t, providertest.Respond(http.StatusOK, `{"status":200,"data":[]}`), providertest.Forbid(t))

	reply := providertest.Call[matchesReply](t, p, "matches", frosty("eu", ""))

	assert.Equal(t, matchesReply{Player: "Frosty#EUW1", Region: "eu", Matches: []matchEntry{}, Empty: true}, reply)
}

func TestAccountEchoesTheLinkedIdentity(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/valorant/v2/account/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, accountBody)
	}), providertest.Forbid(t))

	reply := providertest.Call[gossiprpc.ValorantAccountReply](t, p, "account", frosty("", ""))

	assert.Equal(t, gossiprpc.ValorantAccountReply{
		Player: "Frosty#EUW1", Puuid: "puuid-1", Region: "eu", AccountLevel: 231,
		Card: "https://media.test/card.png", Title: "Vanquisher",
	}, reply)
}

func TestLeaderboardSortsAndCapsTheSlice(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/valorant/v3/leaderboard/ap/console", r.URL.Path,
			"v3 is the platform-aware board; v2 would silently return PC data")
		fmt.Fprint(w, leaderboardBody)
	}), providertest.Forbid(t))

	reply := providertest.Call[leaderboardReply](t, p, "leaderboard", gossiprpc.Request{Region: "AP", Platform: "Console"})

	assert.Equal(t, leaderboardReply{Board: "ap/console", Entries: []lbEntry{
		{Rank: 1, Player: "One#1111", Tier: 27, RR: 910, Wins: 42},
		{Rank: 3, Player: "Secret Agent", Tier: 26, RR: 750, Wins: 33},
		{Rank: 5, Player: "Five#5555", Tier: 25, RR: 700, Wins: 30},
		{Rank: 11, Player: "Eleven#eeee", Tier: 24, RR: 420, Wins: 9},
	}}, reply, "upstream order is not trusted, and a player who hid their name stays hidden")
}

func TestCacheKeysFoldChannelRegionAndPlatform(t *testing.T) {
	store := providertest.NewMemStore()
	p := newProviderWithStore(t, henrik(t, map[string]string{
		"/valorant/v2/account/":     accountBody,
		"/valorant/v3/mmr/":         mmrBody,
		"/valorant/v3/leaderboard/": leaderboardBody,
	}), providertest.Forbid(t), store)

	for _, call := range []struct {
		endpoint string
		req      gossiprpc.Request
	}{
		{"rank", frosty("NA", "")},
		{"rank", gossiprpc.Request{ChannelID: " 1001 ", Region: " Na ", Platform: " Console "}},
		{"account", frosty("kr", "console")},
		{"leaderboard", gossiprpc.Request{Region: "kr"}},
		{"leaderboard", frosty("eu", "Console")},
	} {
		_ = providertest.Endpoint(t, p, call.endpoint)(context.Background(), call.req)
	}

	assert.Equal(t, []string{
		"gossip:valorant:account:1001",
		"gossip:valorant:leaderboard:1001:eu:console",
		"gossip:valorant:leaderboard::kr:pc",
		"gossip:valorant:link:1001",
		"gossip:valorant:rank:1001:na:console",
		"gossip:valorant:rank:1001:na:pc",
	}, store.Keys())
}

func TestProviderDeclaresItsRPCEndpoints(t *testing.T) {
	p := newTestProvider(t, providertest.Forbid(t), providertest.Forbid(t))

	names := make([]string, 0, 7)
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Equal(t, "valorant", p.Name())
	assert.ElementsMatch(t, []string{"rank", "record", "matches", "account", "leaderboard", "link"}, names)
}
