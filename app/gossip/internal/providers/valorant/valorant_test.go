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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProviderWithStore(t testing.TB, henrik, content http.Handler, store *providertest.MemStore) provider.Provider {
	return New(Config{
		BaseURL:        providertest.Upstream(t, henrik),
		ContentBaseURL: providertest.Upstream(t, content),
		APIKey:         "val-key",
	}, providertest.Deps(store))
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
    }
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
      {"leaderboard_rank":3,"name":"Three","tag":"3333","wins":33,"rr":750,"tier":26,"is_anonymized":false}
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

	reply := providertest.Call[rankReply](t, p, "rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "NA"})

	assert.Equal(t, "val-key", gotAuth)
	assert.Equal(t, rankReply{
		Player: "Frosty#EUW1", Region: "na", Tier: "Immortal 1", Elo: 1849, RR: 63,
		LastChange: -12, PeakTier: "Ascendant 2", Placement: 812,
	}, reply, "display preserves name case, region normalizes, the single peak object reads directly")
}

func TestUnrankedAccountsHideTheirEloNoise(t *testing.T) {
	p := newTestProvider(t, providertest.Respond(http.StatusOK,
		`{"status":200,"data":{"current":{"elo":0,"rr":0,"last_change":0,"tier":{"id":0,"name":"UNRANKED"},"leaderboard_placement":{"rank":0}},"peak":{"tier":{"id":0,"name":"UNRANKED"},"rr":0}}}`),
		providertest.Forbid(t))

	reply := providertest.Call[rankReply](t, p, "rank", gossiprpc.Request{Account: "Newbie#EUW", Region: "eu"})

	assert.Equal(t, rankReply{Player: "Newbie#EUW", Region: "eu", Tier: "UNRANKED", Unranked: true}, reply,
		"elo of an unranked account is noise; templates should never see it")
}

func TestRankRejectsMalformedRequestsBeforeAnyUpstreamCall(t *testing.T) {
	const badID = "invalid riot id (want name#tag)"
	const badRegion = "unknown region (want na, eu, ap, kr, br or latam)"
	for _, tc := range []struct {
		name      string
		endpoint  string
		req       gossiprpc.Request
		wantError string
	}{
		{"an empty account", "rank", gossiprpc.Request{}, badID},
		{"an account without a tag", "rank", gossiprpc.Request{Account: "NoTag"}, badID},
		{"an account without a name", "matches", gossiprpc.Request{Account: "#EUW1"}, badID},
		{"an account with an empty tag", "account", gossiprpc.Request{Account: "Name#"}, badID},
		{"an account with an over-long name", "rank", gossiprpc.Request{Account: strings.Repeat("n", 33) + "#tag"}, badID},
		{"a region outside the affinities", "rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "es"}, badRegion},
		{"a spelled-out region", "matches", gossiprpc.Request{Account: "Frosty#EUW1", Region: "north america"}, badRegion},
		{"a shard name instead of an affinity", "rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "euw"}, badRegion},
		{"an unknown platform", "rank", gossiprpc.Request{Account: "Frosty#EUW1", Platform: "mobile"}, "unknown platform (want pc or console)"},
		{"a leaderboard without a region or an account", "leaderboard", gossiprpc.Request{}, "missing region"},
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
	req := gossiprpc.Request{Account: "Ghost404#0000", Region: "ap"}

	first := providertest.Call[rankReply](t, p, "rank", req)
	second := providertest.Call[rankReply](t, p, "rank", req)

	assert.Equal(t, rankReply{Player: "Ghost404#0000", Error: "player not found"}, first)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, upstream.Hits(), "404s are negatively cached for the window")
}

func TestAutoRegionIsResolvedOnceAndSharedAcrossEndpoints(t *testing.T) {
	var counter hitCounter
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/valorant/v2/account/"):
			counter.count("account")
			fmt.Fprint(w, accountBody)
		case strings.HasPrefix(r.URL.Path, "/valorant/v4/matches/"):
			fmt.Fprint(w, `{"status":200,"data":[]}`)
		case strings.HasPrefix(r.URL.Path, "/valorant/v3/mmr/"):
			counter.count("mmr")
			assert.Equal(t, "/valorant/v3/mmr/eu/pc/Frosty/EUW1", r.URL.Path, "the detected region must route the mmr leg")
			fmt.Fprint(w, mmrBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}), providertest.Forbid(t))
	auto := gossiprpc.Request{Account: "Frosty#EUW1"}

	for range 3 {
		reply := providertest.Call[rankReply](t, p, "rank", auto)
		assert.Empty(t, reply.Error)
		assert.Equal(t, "eu", reply.Region)
	}
	assert.Empty(t, providertest.Call[matchesReply](t, p, "matches", auto).Error)
	assert.Equal(t, 1, counter.get("account"),
		"auto-region lookups ride one identity entry; a second account read means the shared resolve was lost")
	assert.Equal(t, 1, counter.get("mmr"), "rank replies collapse onto one cached flight")

	explicit := providertest.Call[rankReply](t, p, "rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "eu"})
	assert.Empty(t, explicit.Error)
	assert.Equal(t, 1, counter.get("account"), "an explicit region never touches the resolve at all")
}

func TestAccountEndpointKeepsItsOwnCacheEntry(t *testing.T) {
	var counter hitCounter
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/valorant/v2/account/") {
			counter.count("account")
			fmt.Fprint(w, accountBody)
			return
		}
		fmt.Fprint(w, mmrBody)
	}), providertest.Forbid(t))
	req := gossiprpc.Request{Account: "Frosty#EUW1"}

	assert.Empty(t, providertest.Call[rankReply](t, p, "rank", req).Error, "warms the identity resolve")
	assert.Empty(t, providertest.Call[accountReply](t, p, "account", req).Error, "must not ride the still-warm resolve entry")
	assert.Equal(t, 2, counter.get("account"))

	assert.Empty(t, providertest.Call[accountReply](t, p, "account", req).Error)
	assert.Equal(t, 2, counter.get("account"), "the account endpoint's own byte-flow cache absorbs the repeat")
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

	reply := providertest.Call[matchesReply](t, p, "matches", gossiprpc.Request{Account: "Frosty#EUW1", Region: "na"})

	assert.Empty(t, reply.Error)
	assert.Equal(t, "mode=competitive&size=5", gotQuery,
		"the competitive-only filter rides the upstream query, not client-side guessing")
	require.Len(t, reply.Matches, 2, "an incomplete game is skipped, not shown as a ghost row")

	win, loss := reply.Matches[0], reply.Matches[1]
	assert.Equal(t, "Ascent", win.Map)
	assert.Equal(t, "Jett", win.Agent)
	assert.Equal(t, "win", win.Result)
	assert.Equal(t, 24, win.Kills)
	assert.InDelta(t, 190.1, win.ACS, 0.001, "score 4563 over 24 rounds, rounded to one decimal")
	assert.GreaterOrEqual(t, win.AgoSeconds, int64(3590))
	assert.Equal(t, "loss", loss.Result)
	assert.InDelta(t, 126.9, loss.ACS, 0.001, "score 2412 over 19 rounds")
}

func TestMatchesEmptyHistoryIsAnAnswerNotAnError(t *testing.T) {
	p := newTestProvider(t, providertest.Respond(http.StatusOK, `{"status":200,"data":[]}`), providertest.Forbid(t))

	reply := providertest.Call[matchesReply](t, p, "matches", gossiprpc.Request{Account: "Quiet#EUW", Region: "eu"})

	assert.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

func TestAccountEchoesResolvedIdentity(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/valorant/v2/account/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, accountBody)
	}), providertest.Forbid(t))

	reply := providertest.Call[accountReply](t, p, "account", gossiprpc.Request{Account: "Frosty#EUW1"})

	assert.Equal(t, accountReply{
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

	assert.Empty(t, reply.Error)
	assert.Equal(t, "ap/console", reply.Board)
	ranks := make([]int, 0, len(reply.Entries))
	for _, entry := range reply.Entries {
		ranks = append(ranks, entry.Rank)
	}
	assert.Equal(t, []int{1, 3, 5, 11}, ranks, "upstream order is not trusted")
	assert.Equal(t, "One#1111", reply.Entries[0].Player)
}

func TestCacheKeysFoldPlayerRegionAndPlatform(t *testing.T) {
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
		{"rank", gossiprpc.Request{Account: "Frosty#EUW1", Region: "NA"}},
		{"rank", gossiprpc.Request{Account: "  FrOsTy # euw1 ", Region: " Na ", Platform: " Console "}},
		{"leaderboard", gossiprpc.Request{Region: "kr"}},
		{"leaderboard", gossiprpc.Request{Account: "  FrOsTy#EUW1  ", Region: "eu", Platform: "Console"}},
	} {
		_ = providertest.Endpoint(t, p, call.endpoint)(context.Background(), call.req)
	}

	assert.Equal(t, []string{
		"gossip:valorant:leaderboard::kr:pc",
		"gossip:valorant:leaderboard:frosty#euw1:eu:console",
		"gossip:valorant:rank:frosty#euw1:na:console",
		"gossip:valorant:rank:frosty#euw1:na:pc",
	}, store.Keys())
}

func TestProviderDeclaresItsRPCEndpoints(t *testing.T) {
	p := newTestProvider(t, providertest.Forbid(t), providertest.Forbid(t))

	names := make([]string, 0, 5)
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Equal(t, "valorant", p.Name())
	assert.ElementsMatch(t, []string{"rank", "matches", "account", "leaderboard", "shop"}, names)
}
