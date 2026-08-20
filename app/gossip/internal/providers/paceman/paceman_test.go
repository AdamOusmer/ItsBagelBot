// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// memStore is an in-memory core.Store for tests, mirroring mcsr's.
type memStore struct{ m map[string][]byte }

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	b, ok := s.m[key]
	return b, ok, nil
}
func (s *memStore) Set(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error { delete(s.m, key); return nil }
func (s *memStore) SetNX(_ context.Context, key string, val []byte, _ time.Duration) (bool, error) {
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = append([]byte(nil), val...)
	return true, nil
}

func newTestProvider(t *testing.T, handler http.Handler) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL},
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

// sessionStatsBody matches getSessionStats/'s envelope, all splits filled.
const sessionStatsBody = `{
	"nether": {"count": 3, "avg": "1:42"},
	"bastion": {"count": 2, "avg": "3:55"},
	"fortress": {"count": 2, "avg": "7:12"},
	"first_portal": {"count": 2, "avg": "9:20"},
	"stronghold": {"count": 1, "avg": "12:05"},
	"end": {"count": 1, "avg": "13:50"},
	"finish": {"count": 0, "avg": "0:00"},
	"truncated": false
}`

const emptySessionStatsBody = `{
	"nether": {"count": 0, "avg": "0:00"},
	"bastion": {"count": 0, "avg": "0:00"},
	"fortress": {"count": 0, "avg": "0:00"},
	"first_portal": {"count": 0, "avg": "0:00"},
	"stronghold": {"count": 0, "avg": "0:00"},
	"end": {"count": 0, "avg": "0:00"},
	"finish": {"count": 0, "avg": "0:00"},
	"truncated": false
}`

const sessionNethersBody = `{"count": 3, "avg": "1:42", "rnph": 21.4, "uuid": "9a8e24df"}`
const untrackedSessionNethersBody = `{"count": 3, "avg": "1:42", "rnph": 0, "uuid": "9a8e24df"}`

func TestSessionParsing(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getSessionStats/":
			assert.Equal(t, "Feinberg", r.URL.Query().Get("name"))
			assert.Equal(t, "6", r.URL.Query().Get("hoursBetween"))
			_, _ = w.Write([]byte(sessionStatsBody))
		case "/getSessionNethers/":
			_, _ = w.Write([]byte(sessionNethersBody))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))

	reply := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Feinberg"}).(gossiprpc.PacemanSessionReply)
	require.Empty(t, reply.Error)
	assert.False(t, reply.Empty)
	assert.Equal(t, 3, reply.NetherCount)
	assert.Equal(t, "1:42", reply.Nether)
	assert.Equal(t, "3:55", reply.Bastion)
	assert.Equal(t, "7:12", reply.Fortress)
	assert.Equal(t, "9:20", reply.FirstPortal)
	assert.Equal(t, "12:05", reply.Stronghold)
	assert.Equal(t, "13:50", reply.End)
	assert.Equal(t, "0:00", reply.Finish)
	assert.Equal(t, 21.4, reply.NPH)
}

func TestSessionEmpty(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getSessionStats/" {
			_, _ = w.Write([]byte(emptySessionStatsBody))
			return
		}
		_, _ = w.Write([]byte(untrackedSessionNethersBody))
	}))

	reply := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Newbie"}).(gossiprpc.PacemanSessionReply)
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty, "zero nether count must read as an empty session, not an error")
}

func TestSessionUpstream4xx(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"unknown player"}`))
	}))

	reply := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "ghost"}).(gossiprpc.PacemanSessionReply)
	assert.Equal(t, "player not found", reply.Error)
}

func TestNethersParsing(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/getSessionNethers/", r.URL.Path)
		_, _ = w.Write([]byte(sessionNethersBody))
	}))

	reply := endpoint(t, p, "nethers")(context.Background(), gossiprpc.Request{Account: "Feinberg"}).(gossiprpc.PacemanNethersReply)
	require.Empty(t, reply.Error)
	assert.False(t, reply.Empty)
	assert.Equal(t, 3, reply.Count)
	assert.Equal(t, "1:42", reply.Avg)
	assert.Equal(t, 21.4, reply.NPH)
}

func TestNethersEmpty(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"count": 0, "avg": "0:00", "rnph": 0}`))
	}))

	reply := endpoint(t, p, "nethers")(context.Background(), gossiprpc.Request{Account: "Newbie"}).(gossiprpc.PacemanNethersReply)
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

func TestNethersUpstream4xx(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))

	reply := endpoint(t, p, "nethers")(context.Background(), gossiprpc.Request{Account: "ghost"}).(gossiprpc.PacemanNethersReply)
	assert.Equal(t, "player not found", reply.Error)
}

func TestLastFortParsing(t *testing.T) {
	start := float64(time.Now().Add(-10 * time.Minute).Unix())
	nether := start + 90
	bastion := start + 165
	fortress := start + 300
	body := `{
		"start": ` + jsonFloat(start) + `,
		"nether": ` + jsonFloat(nether) + `,
		"bastion": ` + jsonFloat(bastion) + `,
		"fortress": ` + jsonFloat(fortress) + `,
		"first_portal": null,
		"stronghold": null
	}`
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/getRecentTimestamps/", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("onlyFort"))
		_, _ = w.Write([]byte("[" + body + "]"))
	}))

	reply := endpoint(t, p, "lastfort")(context.Background(), gossiprpc.Request{Account: "Feinberg"}).(gossiprpc.PacemanLastFortReply)
	require.Empty(t, reply.Error)
	assert.False(t, reply.Empty)
	assert.Equal(t, "1:30", reply.Nether)
	assert.Equal(t, "2:45", reply.Bastion)
	assert.Equal(t, "5:00", reply.Fortress)
	assert.Equal(t, "", reply.FirstPortal, "a null split must render blank, not a bogus duration")
	assert.Equal(t, "", reply.Stronghold)
	// The run started ~10 minutes ago.
	assert.InDelta(t, 600, reply.AgoSeconds, 5)
}

func TestLastFortEmpty(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))

	reply := endpoint(t, p, "lastfort")(context.Background(), gossiprpc.Request{Account: "Feinberg"}).(gossiprpc.PacemanLastFortReply)
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty, "an empty timestamp array must read as no recent fortress pace, not an error")
}

func TestLastFortUpstream4xx(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unknown player"}`))
	}))

	reply := endpoint(t, p, "lastfort")(context.Background(), gossiprpc.Request{Account: "ghost"}).(gossiprpc.PacemanLastFortReply)
	assert.Equal(t, "player not found", reply.Error)
}

func TestMissingAccount(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}))
	reply := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{}).(gossiprpc.PacemanSessionReply)
	assert.Equal(t, "missing account", reply.Error)
}

// jsonFloat renders a float64 without scientific notation for hand-built test
// JSON bodies (Go's default %v can switch to exponent form for large unix
// timestamps, which is not valid input for this purpose but is still valid
// JSON — better to pin the format explicitly).
func jsonFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
