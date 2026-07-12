package clashroyale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
	"ItsBagelBot/app/gateway/internal/provider"
	gatewayrpc "ItsBagelBot/internal/domain/rpc/gateway"

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

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, APIKey: "royale-key"}, provider.Deps{
		Cache: core.NewCache(newMemStore()),
		Log:   zap.NewNop(),
	})
}

func endpoint(t *testing.T, p *Provider, name string) func(context.Context, gatewayrpc.Request) any {
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
	raw, ok := value.(json.RawMessage)
	require.True(t, ok, "unexpected result type %T", value)
	var reply T
	require.NoError(t, json.Unmarshal(raw, &reply))
	return reply
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
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, "/players/#P2LQ0GR", r.URL.Path)
		assert.Equal(t, "Bearer royale-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(playerBody))
	}))

	stats := decodeReply[statsReply](t, endpoint(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: " #p2lq0gr "}))
	require.Empty(t, stats.Error)
	assert.Equal(t, "Bagel", stats.Player)
	assert.Equal(t, "#P2LQ0GR", stats.Tag)
	assert.Equal(t, 62, stats.KingLevel)
	assert.Equal(t, 100, stats.Draws)
	assert.InDelta(t, 60, stats.WinRate, 1e-9)
	assert.Equal(t, "Bakery", stats.Clan.Name)
	assert.Equal(t, "Knight", stats.FavouriteCard.Name)

	decks := decodeReply[decksReply](t, endpoint(t, p, "decks")(context.Background(), gatewayrpc.Request{Account: "P2LQ0GR"}))
	require.Empty(t, decks.Error)
	require.Len(t, decks.CurrentDeck, 8)
	assert.Equal(t, "Knight", decks.CurrentDeck[0].Name)
	assert.Equal(t, "https://example.test/knight.png", decks.CurrentDeck[0].IconURLs.Medium)
	assert.Len(t, decks.SupportCards, 1)
	assert.InDelta(t, 3.75, decks.AverageElixir, 1e-9)

	ranked := decodeReply[rankedReply](t, endpoint(t, p, "ranked")(context.Background(), gatewayrpc.Request{Account: "#P2LQ0GR"}))
	require.Empty(t, ranked.Error)
	assert.False(t, ranked.Unranked)
	assert.Equal(t, 10, ranked.Current.LeagueNumber)
	assert.Equal(t, 2100, ranked.Current.Trophies)
	assert.Equal(t, 321, ranked.Current.Rank)
	assert.Equal(t, 42, ranked.Best.Rank)

	road := decodeReply[trophyRoadReply](t, endpoint(t, p, "trophy_road")(context.Background(), gatewayrpc.Request{Account: "P2LQ0GR"}))
	require.Empty(t, road.Error)
	assert.Equal(t, 9123, road.Trophies)
	assert.Equal(t, 9345, road.BestTrophies)
	assert.Equal(t, "Legendary Arena", road.Arena.Name)

	assert.Equal(t, 1, hits, "all endpoint views must share the profile cache")
}

func TestRankedFallsBackToLeagueStatistics(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"tag":"#P2LQ0GR","name":"Legacy",
			"leagueStatistics":{
				"currentSeason":{"id":"2026-07","trophies":1800,"bestTrophies":1900},
				"previousSeason":{"id":"2026-06","trophies":1700,"rank":900},
				"bestSeason":{"id":"2026-05","trophies":2200,"rank":100}
			}
		}`))
	}))

	reply := decodeReply[rankedReply](t, endpoint(t, p, "ranked")(context.Background(), gatewayrpc.Request{Account: "P2LQ0GR"}))
	require.Empty(t, reply.Error)
	assert.False(t, reply.Unranked)
	assert.Equal(t, "2026-07", reply.Current.SeasonID)
	assert.Equal(t, 1900, reply.Current.BestTrophies)
	assert.Equal(t, "2026-06", reply.Previous.SeasonID)
	assert.Equal(t, "2026-05", reply.Best.SeasonID)
}

func TestMissingAndInvalidTagsDoNotCallUpstream(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("no upstream request expected")
	}))

	missing := decodeReply[statsReply](t, endpoint(t, p, "stats")(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", missing.Error)

	invalid := decodeReply[decksReply](t, endpoint(t, p, "decks")(context.Background(), gatewayrpc.Request{Account: "#ABC123"}))
	assert.Equal(t, "invalid player tag", invalid.Error)
}

func TestPlayerTagNormalizesCommonLetterOToZero(t *testing.T) {
	tag, errMsg := parsePlayerTag(" #p2lqogr ")
	assert.Empty(t, errMsg)
	assert.Equal(t, "#P2LQ0GR", tag.String())
}

func TestNotFoundIsFriendlyAndNegativeCachedAcrossEndpoints(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"reason":"notFound"}`))
	}))

	stats := decodeReply[statsReply](t, endpoint(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: "P2LQ0GR"}))
	assert.Equal(t, "player not found", stats.Error)
	road := decodeReply[trophyRoadReply](t, endpoint(t, p, "trophy_road")(context.Background(), gatewayrpc.Request{Account: "P2LQ0GR"}))
	assert.Equal(t, "player not found", road.Error)
	assert.Equal(t, 1, hits)
}

func TestEndpointNamesAndDefaultConfig(t *testing.T) {
	p := New(Config{APIKey: "key"}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	var names []string
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
		assert.Equal(t, handlerTimeout, ep.Timeout)
	}
	assert.Equal(t, []string{"stats", "decks", "ranked", "trophy_road"}, names)
	assert.Equal(t, "clashroyale", p.Name())
	assert.Equal(t, "https://proxy.royaleapi.dev/v1", defaultBaseURL)
}
