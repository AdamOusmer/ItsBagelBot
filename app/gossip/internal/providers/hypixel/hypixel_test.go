// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hypixel

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func init() { core.SetSSRFCheckForTests(false) }

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

func (s *memStore) SetNX(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = []byte("1")
	return true, nil
}

func newTestProvider(t *testing.T, mojang, hypixel http.Handler) provider.Provider {
	t.Helper()
	mojangSrv := httptest.NewServer(mojang)
	t.Cleanup(mojangSrv.Close)
	hypixelSrv := httptest.NewServer(hypixel)
	t.Cleanup(hypixelSrv.Close)
	return New(Config{BaseURL: hypixelSrv.URL, MojangBaseURL: mojangSrv.URL, APIKey: "hypixel-key"},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

func endpoint(t *testing.T, p provider.Provider, name string) func(context.Context, gossiprpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not declared", name)
	return nil
}

func asReply[T any](t *testing.T, res any) T {
	t.Helper()
	if v, ok := res.(T); ok {
		return v
	}
	raw, ok := res.(codec.RawMessage)
	require.True(t, ok, "unexpected handler result type %T", res)
	var v T
	require.NoError(t, codec.Unmarshal(raw, &v))
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

	reply := asReply[gossiprpc.HypixelStatsReply](t, endpoint(t, p, "stats")(context.Background(), gossiprpc.Request{Account: "Techno"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "hypixel-key", gotKey)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", gotUUID)
	assert.Equal(t, "Techno", reply.Player)
	assert.Equal(t, int64(402), reply.Stars)
	assert.Equal(t, int64(1000), reply.Wins)
	assert.Equal(t, int64(500), reply.FinalDeaths)
}

func TestStatsUUIDSkipsMojang(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("mojang must not be called for a uuid account")
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", r.URL.Query().Get("uuid"))
			_, _ = w.Write([]byte(playerBody))
		}))

	reply := asReply[gossiprpc.HypixelStatsReply](t, endpoint(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "deadbeef-dead-beef-dead-beefdeadbeef"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, int64(402), reply.Stars)
}

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
	h := endpoint(t, p, "stats")

	reply := asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)

	reply = asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.Equal(t, 1, hypixelHits, "unknown player must be served from the negative cache")
}

func TestStatsUnknownNameStopsAtMojang(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessage":"Couldn't find any profile"}`))
		}),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("hypixel must not be called when the name does not resolve")
		}))

	reply := asReply[gossiprpc.HypixelStatsReply](t, endpoint(t, p, "stats")(context.Background(), gossiprpc.Request{Account: "NoSuchName123"}))
	assert.Equal(t, "player not found", reply.Error)
}

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
	h := endpoint(t, p, "stats")

	reply := asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Techno"}))
	assert.Equal(t, "stats lookup not permitted right now", reply.Error)

	reply = asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Techno"}))
	assert.Empty(t, reply.Error)
	assert.Equal(t, int64(402), reply.Stars)
}

func TestMissingAccount(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no upstream call expected") }),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no upstream call expected") }))
	reply := asReply[gossiprpc.HypixelStatsReply](t, endpoint(t, p, "stats")(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

func TestOddRateLimitDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", RateLimit: 100.3},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", MojangRateLimit: 100.3},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
}

func TestMojangCarriesItsOwnBudget(t *testing.T) {
	d := provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()}
	b := provider.NewProvider(providerName, d).Trusted()
	p := newAPI(Config{APIKey: "k"}, d, b)
	assert.NotEqual(t, p.buckets, p.mojangBuckets, "the resolve hop must not share the Hypixel key's bucket")
	assert.NotEqual(t, core.Buckets{}, p.mojangBuckets, "the resolve hop must be metered")
}

func TestStatsMojangRateLimitedPinsBriefly(t *testing.T) {
	var mojangHits int
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mojangHits++
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"TooManyRequestsException"}`))
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(playerBody))
		}))
	h := endpoint(t, p, "stats")

	reply := asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Techno"}))
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", reply.Error)

	reply = asReply[gossiprpc.HypixelStatsReply](t, h(context.Background(), gossiprpc.Request{Account: "Techno"}))
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", reply.Error)
	assert.Equal(t, 1, mojangHits, "a burst during an upstream throttle must answer from the pinned reply, not re-hit the upstream")
}

func TestLooksLikeUUID(t *testing.T) {
	assert.True(t, looksLikeUUID("deadbeefdeadbeefdeadbeefdeadbeef"))
	assert.True(t, looksLikeUUID("deadbeef-dead-beef-dead-beefdeadbeef"))
	assert.False(t, looksLikeUUID("Technoblade"))
	assert.False(t, looksLikeUUID("deadbeef"))
}

func TestUUIDResolvesViaMojang(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/users/profiles/minecraft/Techno", r.URL.Path)
			_, _ = w.Write([]byte(`{"id":"deadbeefdeadbeefdeadbeefdeadbeef","name":"Techno"}`))
		}),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("hypixel must not be called for a uuid resolve")
		}))

	reply := asReply[gossiprpc.HypixelUUIDReply](t, endpoint(t, p, "uuid")(context.Background(), gossiprpc.Request{Account: "Techno"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", reply.UUID)
	assert.Equal(t, "Techno", reply.Player)
}

func TestUUIDSkipsMojangWhenAlreadyUUID(t *testing.T) {
	p := newTestProvider(t,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("mojang must not be called for a uuid account")
		}),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("hypixel must not be called for a uuid resolve")
		}))

	reply := asReply[gossiprpc.HypixelUUIDReply](t, endpoint(t, p, "uuid")(context.Background(),
		gossiprpc.Request{Account: "deadbeef-dead-beef-dead-beefdeadbeef"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeef", reply.UUID)
}

func TestUUIDOnlyWithoutAPIKey(t *testing.T) {
	p := New(Config{}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	var names []string
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Contains(t, names, "uuid")
	assert.NotContains(t, names, "stats")
}
