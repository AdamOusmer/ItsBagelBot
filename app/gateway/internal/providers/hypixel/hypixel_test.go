package hypixel

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
	// Copy: the Store contract says val may come from a pooled buffer the
	// caller recycles as soon as Set returns.
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// newTestProvider wires the provider with BOTH upstreams faked: mojang answers
// the uuid resolve, hypixel answers /v2/player.
func newTestProvider(t *testing.T, mojang, hypixel http.Handler) *Provider {
	t.Helper()
	mojangSrv := httptest.NewServer(mojang)
	t.Cleanup(mojangSrv.Close)
	hypixelSrv := httptest.NewServer(hypixel)
	t.Cleanup(hypixelSrv.Close)
	return New(Config{BaseURL: hypixelSrv.URL, MojangBaseURL: mojangSrv.URL, APIKey: "hypixel-key"},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

func statsHandle(t *testing.T, p *Provider) func(context.Context, gatewayrpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == "stats" {
			return ep.Handle
		}
	}
	t.Fatal("stats endpoint not declared")
	return nil
}

// asReply decodes one handler result (raw wire bytes or typed guard reply).
func asReply(t *testing.T, res any) gatewayrpc.HypixelStatsReply {
	t.Helper()
	if v, ok := res.(gatewayrpc.HypixelStatsReply); ok {
		return v
	}
	raw, ok := res.(json.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v gatewayrpc.HypixelStatsReply
	require.NoError(t, json.Unmarshal(raw, &v))
	return v
}

const playerBody = `{
	"success": true,
	"player": {
		"displayname": "Techno",
		"achievements": {"bedwars_level": 402},
		"stats": {"Bedwars": {
			"wins_bedwars": 1000, "losses_bedwars": 100,
			"final_kills_bedwars": 5000, "final_deaths_bedwars": 500,
			"beds_broken_bedwars": 2000
		}}
	}
}`

func TestStatsResolvesViaMojangThenHypixel(t *testing.T) {
	var gotKey, gotUUID string
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/users/profiles/minecraft/Techno", r.URL.Path)
			_, _ = w.Write([]byte(`{"id":"deadbeefdeadbeefdeadbeefdeadbeef","name":"Techno"}`))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v2/player", r.URL.Path)
			gotUUID = r.URL.Query().Get("uuid")
			gotKey = r.Header.Get("API-Key")
			_, _ = w.Write([]byte(playerBody))
		}))

	reply := asReply(t, statsHandle(t, p)(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "hypixel-key", gotKey)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", gotUUID)
	assert.Equal(t, "Techno", reply.Player)
	assert.Equal(t, int64(402), reply.Stars)
	assert.Equal(t, int64(1000), reply.Wins)
	assert.Equal(t, int64(500), reply.FinalDeaths)
}

// An account that is already a uuid (dashed or not) skips Mojang entirely.
func TestStatsUUIDSkipsMojang(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("mojang must not be called for a uuid account")
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", r.URL.Query().Get("uuid"))
			_, _ = w.Write([]byte(playerBody))
		}))

	reply := asReply(t, statsHandle(t, p)(context.Background(),
		gatewayrpc.Request{Account: "deadbeef-dead-beef-dead-beefdeadbeef"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, int64(402), reply.Stars)
}

// Hypixel answers 200 with player:null for an unknown uuid; that must chat
// "player not found" and negative-cache (the second call hits no upstream).
func TestStatsUnknownPlayerNegativeCached(t *testing.T) {
	var hypixelHits int
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":"deadbeefdeadbeefdeadbeefdeadbeef","name":"Ghosty"}`))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hypixelHits++
			_, _ = w.Write([]byte(`{"success": true, "player": null}`))
		}))
	h := statsHandle(t, p)

	reply := asReply(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)

	reply = asReply(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.Equal(t, 1, hypixelHits, "unknown player must be served from the negative cache")
}

// A name Mojang does not know 404s at the resolve step and negative-caches
// without ever spending the Hypixel budget.
func TestStatsUnknownNameStopsAtMojang(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessage":"Couldn't find any profile"}`))
		}),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("hypixel must not be called when the name does not resolve")
		}))

	reply := asReply(t, statsHandle(t, p)(context.Background(), gatewayrpc.Request{Account: "NoSuchName123"}))
	assert.Equal(t, "player not found", reply.Error)
}

// A key-permission failure (403) must answer the config-flavored message and
// NOT be pinned per player.
func TestStatsForbiddenFriendlyNotCached(t *testing.T) {
	var hits int
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":"deadbeefdeadbeefdeadbeefdeadbeef","name":"Techno"}`))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits++
			if hits == 1 {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"cause":"Invalid API key"}`))
				return
			}
			_, _ = w.Write([]byte(playerBody))
		}))
	h := statsHandle(t, p)

	reply := asReply(t, h(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	assert.Equal(t, "stats lookup not permitted right now", reply.Error)

	// Key fixed: the very next request retries upstream instead of serving a
	// cached denial.
	reply = asReply(t, h(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	assert.Empty(t, reply.Error)
	assert.Equal(t, int64(402), reply.Stars)
}

func TestMissingAccount(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no upstream call expected") }),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no upstream call expected") }))
	reply := asReply(t, statsHandle(t, p)(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

func TestOddRateLimitDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", RateLimit: 100.3},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
}

func TestLooksLikeUUID(t *testing.T) {
	assert.True(t, looksLikeUUID("deadbeefdeadbeefdeadbeefdeadbeef"))
	assert.True(t, looksLikeUUID("deadbeef-dead-beef-dead-beefdeadbeef"))
	assert.False(t, looksLikeUUID("Technoblade"))
	assert.False(t, looksLikeUUID("deadbeef"))
}
