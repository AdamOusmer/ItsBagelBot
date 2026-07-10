package fortnite

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

// memStore is an in-memory core.Store for tests.
type memStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return b, ok, nil
}
func (s *memStore) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func newTestProvider(t *testing.T, upstream http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, APIKey: "fortnite-key"},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

func handle(t *testing.T, p *Provider, name string) func(context.Context, gatewayrpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("%s endpoint not declared", name)
	return nil
}

// asReply decodes one handler result (raw wire bytes or typed guard reply).
func asReply[T any](t *testing.T, res any) T {
	t.Helper()
	if v, ok := res.(T); ok {
		return v
	}
	raw, ok := res.(json.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v T
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

// asStats / asShop are the endpoint-typed instantiations the tests read with.
var (
	asStats = asReply[gatewayrpc.FortniteStatsReply]
	asShop  = asReply[gatewayrpc.FortniteShopReply]
)

const statsBody = `{
	"status": 200,
	"data": {
		"account": {"id": "abc", "name": "Ninja"},
		"stats": {"all": {
			"overall": {"wins": 301, "matches": 6232, "kills": 21679, "kd": 3.66, "winRate": 4.83},
			"solo": {"wins": 120, "matches": 2400, "kills": 9000, "kd": 3.2, "winRate": 5.0},
			"duo": {"wins": 90, "matches": 1900, "kills": 7000, "kd": 3.8, "winRate": 4.7},
			"squad": null
		}}
	}
}`

func TestStatsNormalizesAndPassesParams(t *testing.T) {
	var gotAuth, gotName, gotType, gotWindow string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/stats/br/v2", r.URL.Path)
		gotAuth = r.Header.Get("Authorization")
		q := r.URL.Query()
		gotName, gotType, gotWindow = q.Get("name"), q.Get("accountType"), q.Get("timeWindow")
		assert.Equal(t, "none", q.Get("image"))
		_, _ = w.Write([]byte(statsBody))
	}))

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", AccountType: "psn", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "fortnite-key", gotAuth)
	assert.Equal(t, "Ninja", gotName)
	assert.Equal(t, "psn", gotType)
	assert.Equal(t, "season", gotWindow)

	assert.Equal(t, "Ninja", reply.Player)
	assert.Equal(t, "season", reply.Window)
	assert.Equal(t, int64(301), reply.Overall.Wins)
	assert.Equal(t, int64(6232), reply.Overall.Matches)
	assert.Equal(t, int64(21679), reply.Overall.Kills)
	assert.InDelta(t, 3.66, reply.Overall.KD, 1e-9)
	assert.InDelta(t, 4.83, reply.Overall.WinRate, 1e-9)
	assert.Equal(t, int64(120), reply.Solo.Wins)
	assert.Equal(t, int64(90), reply.Duo.Wins)
	// A queue the player never touched is null upstream and zero in the reply.
	assert.Zero(t, reply.Squad.Wins)
	assert.Zero(t, reply.Squad.Matches)
}

// Blank/garbage account type and window fall back to epic + lifetime.
func TestStatsDefaultsEpicLifetime(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "epic", q.Get("accountType"))
		assert.Equal(t, "lifetime", q.Get("timeWindow"))
		_, _ = w.Write([]byte(statsBody))
	}))

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", AccountType: "steam", TimeWindow: "weekly"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "lifetime", reply.Window)
}

// An unknown player 404s upstream, chats "player not found" and
// negative-caches (the second call hits no upstream).
func TestStatsUnknownPlayerNegativeCached(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"error":"Account not found."}`))
	}))
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "Account not found.", reply.Error)

	reply = asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "Account not found.", reply.Error)
	assert.Equal(t, 1, hits, "unknown player must be served from the negative cache")
}

// A 403 for an account whose public-stats toggle is off chats the specific
// private-stats message (and negative-caches like a missing player).
func TestStatsPrivateAccountFriendly(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":403,"error":"This account's public game stats are disabled."}`))
	}))

	reply := asStats(t, handle(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: "Shy"}))
	assert.Equal(t, "this player's game stats are private", reply.Error)
}

// A 403 that is a key problem (not the stats toggle) answers the generic
// config-flavored message and is NOT cached: the next request retries.
func TestStatsBadKeyNotCached(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":403,"error":"Invalid authorization header."}`))
			return
		}
		_, _ = w.Write([]byte(statsBody))
	}))
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja"}))
	assert.Equal(t, "stats lookup not permitted right now", reply.Error)

	reply = asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja"}))
	assert.Empty(t, reply.Error)
	assert.Equal(t, int64(301), reply.Overall.Wins)
}

func TestStatsMissingAccount(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}))
	reply := asStats(t, handle(t, p, "stats")(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

const shopBody = `{
	"status": 200,
	"data": {
		"date": "2026-07-09T00:00:00Z",
		"entries": [
			{"finalPrice": 2800, "bundle": {"name": "Peely Bundle"}, "brItems": [{"name": "Peely"}]},
			{"finalPrice": 1200, "brItems": [{"name": "Renegade Raider"}]},
			{"finalPrice": 500, "tracks": [{"title": "Never Gonna Give You Up"}]},
			{"finalPrice": 400}
		]
	}
}`

func TestShopNormalizesAndCaches(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		require.Equal(t, "/v2/shop", r.URL.Path)
		_, _ = w.Write([]byte(shopBody))
	}))
	h := handle(t, p, "shop")

	reply := asShop(t, h(context.Background(), gatewayrpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "2026-07-09", reply.Date)
	// The nameless entry is dropped; the bundle keeps its bundle name.
	assert.Equal(t, 3, reply.Count)
	require.Len(t, reply.Entries, 3)
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Peely Bundle", Price: 2800}, reply.Entries[0])
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Renegade Raider", Price: 1200}, reply.Entries[1])
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Never Gonna Give You Up", Price: 500}, reply.Entries[2])

	// Second call is served from the cache.
	reply = asShop(t, h(context.Background(), gatewayrpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 1, hits)
}

func TestOddRateLimitDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", RateLimit: 100.3},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
}

func TestNormalizers(t *testing.T) {
	assert.Equal(t, "epic", normalizeAccountType(""))
	assert.Equal(t, "epic", normalizeAccountType("steam"))
	assert.Equal(t, "psn", normalizeAccountType(" PSN "))
	assert.Equal(t, "xbl", normalizeAccountType("xbl"))
	assert.Equal(t, "lifetime", normalizeTimeWindow(""))
	assert.Equal(t, "season", normalizeTimeWindow("Season"))
	assert.Equal(t, "lifetime", normalizeTimeWindow("weekly"))
}
