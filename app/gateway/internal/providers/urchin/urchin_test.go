package urchin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gateway/internal/core"
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
	s.m[key] = val
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
	return New(Config{BaseURL: srv.URL, APIKey: "test-key"}, core.NewCache(newMemStore()), nil, zap.NewNop())
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

	reply := endpoint(t, p, "daily")(context.Background(), gatewayrpc.Request{Account: "Techno"}).(gatewayrpc.UrchinSessionReply)
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

	reply := endpoint(t, p, "weekly")(context.Background(), gatewayrpc.Request{Account: "x"}).(gatewayrpc.UrchinSessionReply)
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

	_ = h(context.Background(), gatewayrpc.Request{Account: "Techno"})
	// Same player, case-insensitive: served from cache.
	_ = h(context.Background(), gatewayrpc.Request{Account: "techno"})
	assert.Equal(t, 1, hits)
}

func TestPlayerNotFoundIsReplyError(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"player not found"}`))
	}))

	reply := endpoint(t, p, "daily")(context.Background(), gatewayrpc.Request{Account: "ghost"}).(gatewayrpc.UrchinSessionReply)
	assert.Equal(t, "player not found", reply.Error)
}

func TestStatsParsing(t *testing.T) {
	body := `{
		"uuid": "abc", "username": "Techno", "displayname": "Techno", "slim": false, "tags": [],
		"hypixel": {
			"achievements": {"bedwars_level": 402},
			"stats": {"Bedwars": {
				"wins_bedwars": 1000, "losses_bedwars": 100,
				"final_kills_bedwars": 5000, "final_deaths_bedwars": 500,
				"beds_broken_bedwars": 2000
			}}
		}
	}`
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/player/profile", r.URL.Path)
		assert.NotEmpty(t, r.URL.Query().Get("max_cache_age"))
		_, _ = w.Write([]byte(body))
	}))

	reply := endpoint(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: "Techno"}).(gatewayrpc.UrchinStatsReply)
	require.Empty(t, reply.Error)
	assert.Equal(t, int64(402), reply.Stars)
	assert.Equal(t, int64(1000), reply.Wins)
	assert.Equal(t, int64(5000), reply.FinalKills)
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

	reply := endpoint(t, p, "sniper")(context.Background(), gatewayrpc.Request{Account: "Aim"}).(gatewayrpc.UrchinSniperReply)
	require.Empty(t, reply.Error)
	assert.Equal(t, 7.5, reply.Score)
	assert.Equal(t, "warn", reply.Mode)
	assert.Equal(t, 1, reply.TagCount)
}

func TestTagsParsing(t *testing.T) {
	body := `{"uuid":"abc","displayname":"Sus","tags":[
		{"tag_type":"cheater","reason":"bhop","added_by":1,"added_on":0,"hide_username":false},
		{"tag_type":"sniper","reason":"","added_by":1,"added_on":0,"hide_username":false}
	]}`
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	reply := endpoint(t, p, "tags")(context.Background(), gatewayrpc.Request{Account: "Sus"}).(gatewayrpc.UrchinTagsReply)
	require.Empty(t, reply.Error)
	require.Len(t, reply.Tags, 2)
	assert.Equal(t, gatewayrpc.UrchinTag{Type: "cheater", Reason: "bhop"}, reply.Tags[0])
	assert.Equal(t, gatewayrpc.UrchinTag{Type: "sniper"}, reply.Tags[1])
}

func TestMissingAccount(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}))
	reply := endpoint(t, p, "daily")(context.Background(), gatewayrpc.Request{}).(gatewayrpc.UrchinSessionReply)
	assert.Equal(t, "missing account", reply.Error)
}
