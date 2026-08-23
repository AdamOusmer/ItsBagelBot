// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return append([]byte(nil), b...), ok, nil
}

func (s *memStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), value...)
	return nil
}

func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memStore) SetNX(_ context.Context, key string, val []byte, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = append([]byte(nil), val...)
	return true, nil
}

func newTestProvider(t *testing.T, henrik, content http.Handler) provider.Provider {
	t.Helper()
	henrikSrv := httptest.NewServer(henrik)
	t.Cleanup(henrikSrv.Close)
	contentSrv := httptest.NewServer(content)
	t.Cleanup(contentSrv.Close)
	return New(Config{
		BaseURL:        henrikSrv.URL,
		ContentBaseURL: contentSrv.URL,
		APIKey:         "val-key",
	}, provider.Deps{
		Cache: core.NewCache(newMemStore()),
		Log:   zap.NewNop(),
	})
}

func noUpstream(t *testing.T, name string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected %s request: %s", name, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	})
}

func endpoint(t *testing.T, p provider.Provider, name string) func(context.Context, gossiprpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not found", name)
	return nil
}

func decodeReply[T any](t *testing.T, value any) T {
	t.Helper()
	if typed, ok := value.(T); ok {
		return typed
	}
	raw, ok := value.(codec.RawMessage)
	require.True(t, ok, "unexpected result type %T", value)
	var reply T
	require.NoError(t, codec.Unmarshal(raw, &reply))
	return reply
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
    "peak":[
      {"season":{"id":"ab57","short":"S25"},"ranking_schema":"Competitive","tier":{"id":21,"name":"Ascendant 2"},"rr":80},
      {"season":{"id":"cd68","short":"S26"},"ranking_schema":"Competitive","tier":{"id":23,"name":"Immortal 1"},"rr":10}
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

func TestRankFetchesMMRWithPlainAuthorizationHeader(t *testing.T) {
	// HenrikDev takes the raw key, not "Bearer <key>"; this pins the exact
	// header because a Bearer prefix yields a 401 that looks like a bad key.
	var gotAuth string
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		assert.Equal(t, "/valorant/v3/mmr/na/pc/Frosty/EUW1", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, mmrBody)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[rankReply](t, endpoint(t, p, "rank")(context.Background(), gossiprpc.Request{
		Account: "Frosty#EUW1",
		Region:  "NA",
	}))

	assert.Empty(t, reply.Error)
	assert.Equal(t, "Frosty#EUW1", reply.Player, "display preserves name case, canonical tag")
	assert.Equal(t, "na", reply.Region, "region normalizes to canonical form")
	assert.Equal(t, "Immortal 1", reply.Tier)
	assert.Equal(t, 1849, reply.Elo)
	assert.Equal(t, 63, reply.RR)
	assert.Equal(t, -12, reply.LastChange)
	assert.Equal(t, 812, reply.Placement)
	assert.False(t, reply.Unranked)
	assert.Equal(t, "Immortal 1", reply.PeakTier, "peak picks highest tier id, not newest season")
	assert.Equal(t, "val-key", gotAuth)
}

func TestRankAutoRegionResolvesThroughSharedAccountEntry(t *testing.T) {
	var mu sync.Mutex
	accountHits, mmrHits := 0, 0
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/valorant/v2/account/"):
			accountHits++
			fmt.Fprint(w, accountBody)
		case strings.HasPrefix(r.URL.Path, "/valorant/v3/mmr/"):
			mmrHits++
			assert.Equal(t, "/valorant/v3/mmr/eu/pc/Frosty/EUW1", r.URL.Path,
				"detected region must route the mmr leg")
			fmt.Fprint(w, mmrBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))
	handle := endpoint(t, p, "rank")
	req := gossiprpc.Request{Account: "Frosty#EUW1"}

	for i := 0; i < 3; i++ {
		reply := decodeReply[rankReply](t, handle(context.Background(), req))
		assert.Empty(t, reply.Error)
		assert.Equal(t, "eu", reply.Region)
	}
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, accountHits, "identity resolve is paid once, then cached for the day")
	assert.Equal(t, 1, mmrHits, "rank replies collapse onto one cached flight")
}

func TestRankNegativeCacheStopsRepeatUpstreamHits(t *testing.T) {
	hits := 0
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"status":404,"errors":[{"code":"NO_ACCOUNT","message":"No account found"}]}`)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	handle := endpoint(t, p, "rank")
	req := gossiprpc.Request{Account: "Ghost404#0000", Region: "ap"}
	first := decodeReply[rankReply](t, handle(context.Background(), req))
	second := decodeReply[rankReply](t, handle(context.Background(), req))

	assert.Equal(t, "player not found", first.Error)
	assert.Equal(t, first.Error, second.Error)
	assert.Equal(t, 1, hits, "404s are negatively cached for the window")
}

func TestUnrankedZeroesEloButKeepsRRShape(t *testing.T) {
	unrankedBody := `{"status":200,"data":{"current":{"elo":0,"rr":0,"last_change":0,"tier":{"id":0,"name":"UNRANKED"},"leaderboard_placement":{"rank":0}},"peak":[]}}`
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, unrankedBody)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[rankReply](t, endpoint(t, p, "rank")(context.Background(), gossiprpc.Request{
		Account: "Newbie#EUW", Region: "eu",
	}))
	assert.True(t, reply.Unranked)
	assert.Zero(t, reply.Elo, "elo of an unranked account is noise; templates should never see it")
	assert.Empty(t, reply.PeakTier)
}

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

func TestMatchesSummarizesCompletedGamesOnly(t *testing.T) {
	anHourAgo := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(matchesBody, anHourAgo, anHourAgo, anHourAgo)
	var gotQuery string
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		assert.Equal(t, "/valorant/v4/matches/na/pc/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, body)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[matchesReply](t, endpoint(t, p, "matches")(context.Background(), gossiprpc.Request{
		Account: "Frosty#EUW1", Region: "na",
	}))

	assert.Empty(t, reply.Error)
	assert.Equal(t, "mode=competitive&size=5", gotQuery,
		"the competitive-only filter rides the upstream query, not client-side guessing")
	require.Len(t, reply.Matches, 2, "an incomplete game is skipped, not shown as a ghost row")

	win := reply.Matches[0]
	assert.Equal(t, "Ascent", win.Map)
	assert.Equal(t, "Jett", win.Agent)
	assert.Equal(t, "win", win.Result)
	assert.Equal(t, 24, win.Kills)
	assert.InDelta(t, 190.1, win.ACS, 0.001, "score 4563 over 24 rounds, rounded to one decimal")
	assert.GreaterOrEqual(t, win.AgoSeconds, int64(3590))

	loss := reply.Matches[1]
	assert.Equal(t, "loss", loss.Result)
	assert.InDelta(t, 126.9, loss.ACS, 0.001, "score 2412 over 19 rounds")
}

func TestMatchesEmptyHistoryIsAnAnswerNotAnError(t *testing.T) {
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":200,"data":[]}`)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[matchesReply](t, endpoint(t, p, "matches")(context.Background(), gossiprpc.Request{
		Account: "Quiet#EUW", Region: "eu",
	}))
	assert.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

func TestAccountEchoesResolvedIdentity(t *testing.T) {
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/valorant/v2/account/Frosty/EUW1", r.URL.Path)
		fmt.Fprint(w, accountBody)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[accountReply](t, endpoint(t, p, "account")(context.Background(), gossiprpc.Request{
		Account: "Frosty#EUW1",
	}))
	assert.Empty(t, reply.Error)
	assert.Equal(t, "Frosty#EUW1", reply.Player)
	assert.Equal(t, "puuid-1", reply.Puuid)
	assert.Equal(t, "eu", reply.Region)
	assert.Equal(t, 231, reply.AccountLevel)
	assert.Equal(t, "https://media.test/card.png", reply.Card)
}

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

func TestLeaderboardSortsAndCapsTheSlice(t *testing.T) {
	henrik := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/valorant/v3/leaderboard/ap/console", r.URL.Path,
			"v3 is the platform-aware board; v2 would silently return PC data")
		fmt.Fprint(w, leaderboardBody)
	})
	p := newTestProvider(t, henrik, noUpstream(t, "content"))

	reply := decodeReply[leaderboardReply](t, endpoint(t, p, "leaderboard")(context.Background(), gossiprpc.Request{
		Region:   "AP",
		Platform: "Console",
	}))

	assert.Empty(t, reply.Error)
	assert.Equal(t, "ap/console", reply.Board)
	ranks := make([]int, 0, len(reply.Entries))
	for _, entry := range reply.Entries {
		ranks = append(ranks, entry.Rank)
	}
	assert.Equal(t, []int{1, 3, 5, 11}, ranks, "upstream order is not trusted")
	assert.Equal(t, "One#1111", reply.Entries[0].Player)
}

func TestLeaderboardRequiresRegionWithoutAccount(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))

	reply := decodeReply[leaderboardReply](t, endpoint(t, p, "leaderboard")(context.Background(), gossiprpc.Request{}))
	assert.Contains(t, reply.Error, "missing region")
}

func TestIdentityValidationAndCacheKeys(t *testing.T) {
	t.Run("parseRiotID", func(t *testing.T) {
		cases := []struct {
			in     string
			wantID string
			reject string
		}{
			{in: "Frosty#EUW1", wantID: "Frosty#EUW1"},
			{in: " frosty#euw1 ", wantID: "frosty#EUW1"},
			{in: "", reject: "invalid riot id"},
			{in: "NoTag", reject: "invalid riot id"},
			{in: "#EUW1", reject: "invalid riot id"},
			{in: "Name#", reject: "invalid riot id"},
			{in: strings.Repeat("n", 33) + "#tag", reject: "invalid riot id"},
		}
		for _, tc := range cases {
			id, msg := parseRiotID(tc.in)
			if tc.reject != "" {
				assert.NotEmpty(t, msg, tc.in)
				continue
			}
			assert.Empty(t, msg, tc.in)
			assert.Equal(t, tc.wantID, id.String(), tc.in)
		}
	})

	t.Run("normalizeRegion", func(t *testing.T) {
		region, msg := normalizeRegion(" NA ")
		assert.Equal(t, "na", region)
		assert.Empty(t, msg)

		region, msg = normalizeRegion("")
		assert.Equal(t, "auto", region, "empty means detect-from-account")
		assert.Empty(t, msg)

		for _, bad := range []string{"es", "north america", "euw"} {
			_, msg = normalizeRegion(bad)
			assert.NotEmpty(t, msg, bad)
		}
	})

	t.Run("normalizePlatform", func(t *testing.T) {
		platform, msg := normalizePlatform("")
		assert.Equal(t, "pc", platform, "pc is the default split")
		assert.Empty(t, msg)
		platform, msg = normalizePlatform("Console")
		assert.Equal(t, "console", platform)
		assert.Empty(t, msg)
		_, msg = normalizePlatform("mobile")
		assert.NotEmpty(t, msg)
	})

	t.Run("riotID folds scoping into the cache key", func(t *testing.T) {
		id, msg := riotID(gossiprpc.Request{Account: "Frosty#EUW1", Region: "NA"})
		assert.Empty(t, msg)
		assert.Equal(t, "frosty#euw1:na:pc", id.Key,
			"same player, different spelling and region casing must share one entry")

		id, msg = riotID(gossiprpc.Request{Account: "Frosty#EUW1"})
		assert.Empty(t, msg)
		assert.Equal(t, "frosty#euw1:auto:pc", id.Key,
			"unset region keys separately from an explicit one; detection fills the gap")

		_, msg = riotID(gossiprpc.Request{Account: "Frosty#EUW1", Platform: "stadia"})
		assert.NotEmpty(t, msg)
	})
}

func TestEndpointSurface(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "henrik"), noUpstream(t, "content"))
	assert.Equal(t, "valorant", p.Name())
	names := make([]string, 0, 5)
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.ElementsMatch(t, []string{"rank", "matches", "account", "leaderboard", "shop"}, names)
}
