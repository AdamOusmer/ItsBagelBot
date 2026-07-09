package urchin

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

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, APIKey: "test-key"},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

func endpoint(t *testing.T, p *Provider, name string) func(context.Context, gatewayrpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not declared", name)
	return nil
}

// asReply decodes one handler result into T. Byte-flow endpoints answer
// pre-marshaled wire bytes (json.RawMessage); guard-path failures answer the
// typed reply directly. Both decode the same on the wire.
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

const sessionBody = `{
	"uuid": "abc",
	"displayname": "§7Techno",
	"from": 1720000000000,
	"from_readable": "today",
	"delta": {
		"stats": {"Bedwars": {
			"wins_bedwars": 5,
			"losses_bedwars": 2,
			"final_kills_bedwars": 21,
			"final_deaths_bedwars": 3,
			"beds_broken_bedwars": 9,
			"games_played_bedwars": 8,
			"Experience": 4870.5
		}},
		"achievements": {"bedwars_level": 1}
	}
}`

func TestDailySessionParsing(t *testing.T) {
	var gotKey, gotPlayer string
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/player/sessions/daily", r.URL.Path)
		gotKey = r.Header.Get("X-API-Key")
		gotPlayer = r.URL.Query().Get("player")
		_, _ = w.Write([]byte(sessionBody))
	}))

	reply := asReply[gatewayrpc.UrchinSessionReply](t, endpoint(t, p, "daily")(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "test-key", gotKey)
	assert.Equal(t, "Techno", gotPlayer)
	assert.Equal(t, "Techno", reply.Player)
	assert.Equal(t, int64(1720000000), reply.SinceUnix)
	assert.Equal(t, int64(5), reply.Wins)
	assert.Equal(t, int64(2), reply.Losses)
	assert.Equal(t, int64(21), reply.FinalKills)
	assert.Equal(t, int64(3), reply.FinalDeaths)
	assert.Equal(t, int64(9), reply.BedsBroken)
	assert.Equal(t, int64(8), reply.GamesPlayed)
	assert.Equal(t, int64(1), reply.Levels)
}

func TestSessionObjectDeltaSkipped(t *testing.T) {
	// A returning player diffed against a partial snapshot surfaces totals as
	// {old:null,new:N}; those must read 0, not the lifetime total.
	body := `{"uuid":"abc","from":0,"from_readable":"x","delta":{"stats":{"Bedwars":{"wins_bedwars":{"old":null,"new":5000}}}}}`
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	reply := asReply[gatewayrpc.UrchinSessionReply](t, endpoint(t, p, "weekly")(context.Background(), gatewayrpc.Request{Account: "x"}))
	require.Empty(t, reply.Error)
	assert.Zero(t, reply.Wins)
}

func TestSessionCachesReply(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(sessionBody))
	}))
	h := endpoint(t, p, "daily")

	first := asReply[gatewayrpc.UrchinSessionReply](t, h(context.Background(), gatewayrpc.Request{Account: "Techno"}))
	// Same player, case-insensitive: served from cache, byte-identical.
	second := asReply[gatewayrpc.UrchinSessionReply](t, h(context.Background(), gatewayrpc.Request{Account: "techno"}))
	assert.Equal(t, 1, hits)
	assert.Equal(t, first, second)
}

// A cache hit answers pre-marshaled bytes: the second call must be a raw
// passthrough, not a re-marshaled struct.
func TestSessionHitIsRawBytes(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sessionBody))
	}))
	h := endpoint(t, p, "daily")

	_ = h(context.Background(), gatewayrpc.Request{Account: "Techno"})
	res := h(context.Background(), gatewayrpc.Request{Account: "Techno"})
	_, isRaw := res.(json.RawMessage)
	assert.True(t, isRaw, "cache hit must answer stored wire bytes")
}

// A 404 negative-caches the FRIENDLY REPLY itself: the second lookup answers
// from the store with no upstream hit.
func TestPlayerNotFoundNegativeCached(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"player not found"}`))
	}))
	h := endpoint(t, p, "daily")

	reply := asReply[gatewayrpc.UrchinSessionReply](t, h(context.Background(), gatewayrpc.Request{Account: "ghost"}))
	assert.Equal(t, "player not found", reply.Error)

	reply = asReply[gatewayrpc.UrchinSessionReply](t, h(context.Background(), gatewayrpc.Request{Account: "ghost"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.Equal(t, 1, hits, "the miss must be served from the negative cache")
}

func TestSniperResolvesUUIDThenScores(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/player/tags":
			_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Aim","tags":[]}`))
		case "/v3/cubelify":
			assert.Equal(t, "deadbeef", r.URL.Query().Get("uuid"))
			assert.Equal(t, "test-key", r.URL.Query().Get("key"))
			_, _ = w.Write([]byte(`{"score":{"value":7.5,"mode":"warn"},"tags":[{"icon":"x","color":1,"tooltip":"t"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))

	reply := asReply[gatewayrpc.UrchinSniperReply](t, endpoint(t, p, "sniper")(context.Background(), gatewayrpc.Request{Account: "Aim"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 7.5, reply.Score)
	assert.Equal(t, "warn", reply.Mode)
	assert.Equal(t, 1, reply.TagCount)
}

// An empty uuid on a 200 tags reply must read as "player not found", never as a
// cacheable success that would poison the downstream cubelify call.
func TestResolveEmptyUUIDIsNotFound(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/player/tags", r.URL.Path)
		_, _ = w.Write([]byte(`{"uuid":"","displayname":null,"tags":[]}`))
	}))

	reply := asReply[gatewayrpc.UrchinSniperReply](t, endpoint(t, p, "sniper")(context.Background(), gatewayrpc.Request{Account: "ghost"}))
	assert.Equal(t, "player not found", reply.Error)
}

func TestTagsParsing(t *testing.T) {
	body := `{"uuid":"abc","displayname":"Sus","tags":[
		{"tag_type":"cheater","reason":"bhop","added_by":1,"added_on":0,"hide_username":false},
		{"tag_type":"sniper","reason":"","added_by":1,"added_on":0,"hide_username":false}
	]}`
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	reply := asReply[gatewayrpc.UrchinTagsReply](t, endpoint(t, p, "tags")(context.Background(), gatewayrpc.Request{Account: "Sus"}))
	require.Empty(t, reply.Error)
	require.Len(t, reply.Tags, 2)
	assert.Equal(t, gatewayrpc.UrchinTag{Type: "cheater", Reason: "bhop"}, reply.Tags[0])
	assert.Equal(t, gatewayrpc.UrchinTag{Type: "sniper"}, reply.Tags[1])
}

// !tag and !sniper both need /v3/player/tags — !tag for the blacklist tags,
// !sniper only for the uuid it feeds cubelify. The shared cache must collapse
// them onto ONE upstream fetch regardless of which command runs first.
func TestTagsAndSniperShareUpstreamFetch(t *testing.T) {
	run := func(t *testing.T, first, second string) {
		var tagsHits, cubelifyHits int
		p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v3/player/tags":
				tagsHits++
				_, _ = w.Write([]byte(`{"uuid":"deadbeef","displayname":"Aim","tags":[]}`))
			case "/v3/cubelify":
				cubelifyHits++
				_, _ = w.Write([]byte(`{"score":{"value":3,"mode":"ok"},"tags":[]}`))
			default:
				t.Errorf("unexpected path %s", r.URL.Path)
			}
		}))
		_ = endpoint(t, p, first)(context.Background(), gatewayrpc.Request{Account: "Aim"})
		_ = endpoint(t, p, second)(context.Background(), gatewayrpc.Request{Account: "Aim"})
		assert.Equal(t, 1, tagsHits, "the /v3/player/tags fetch must be shared, not repeated")
		assert.Equal(t, 1, cubelifyHits)
	}
	t.Run("tag then sniper", func(t *testing.T) { run(t, "tags", "sniper") })
	t.Run("sniper then tag", func(t *testing.T) { run(t, "sniper", "tags") })
}

func TestMissingAccount(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}))
	reply := asReply[gatewayrpc.UrchinSessionReply](t, endpoint(t, p, "daily")(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

// Odd configured rate limits must floor, not panic the boot (NewSpec requires
// integer capacities; 550.5 * 0.75 style fractions crashlooped otherwise).
func TestOddRateLimitDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", RateLimit: 550.5},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
}
