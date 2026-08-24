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

// The SSRF gate refuses plain-http loopback fakes; these tests predate it and
// dial httptest servers, so the process-wide test switch turns the gate off.
// The gate's own semantics are pinned by core's table tests.
func init() { core.SetSSRFCheckForTests(false) }

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

// newTestProviderPB is newTestProvider but also points UserBaseURL at the
// fake server: personal_best is the one endpoint that calls the api/us host
// instead of stats/api, so its tests need both bases wired to the same
// httptest server (routed by path, like every other test in this file).
func newTestProviderPB(t *testing.T, handler http.Handler) provider.Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, UserBaseURL: srv.URL},
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

// pbBody mirrors the real /user?name=&sortByTime=1 envelope confirmed by
// hand (curl) against paceman.gg: pbs is null per-window when the player has
// no best there yet, and a present entry only carries a "time" gossip reads.
const pbBody = `{
	"user": {"uuid": "9a8e", "twitchId": "1", "daily": 0, "weekly": 0, "monthly": 0, "bonus": 1, "score": 1},
	"completions": [],
	"pbs": {
		"daily": {"_id": "1", "submitted": 1716945000000, "time": 400123},
		"weekly": {"_id": "2", "submitted": 1716945000000, "time": 390456},
		"monthly": {"_id": "3", "submitted": 1716945000000, "time": 380789},
		"allTime": {"_id": "4", "submitted": 1716945000000, "time": 370012}
	}
}`

const pbEmptyBody = `{
	"user": {"uuid": "9a8e", "twitchId": "1", "daily": 0, "weekly": 0, "monthly": 0, "bonus": 1, "score": 1},
	"completions": [],
	"pbs": {"daily": null, "weekly": null, "monthly": null, "allTime": null}
}`

func TestPersonalBestWindows(t *testing.T) {
	cases := []struct {
		window string
		want   string
	}{
		{"daily", "6:40.123"},
		{"weekly", "6:30.456"},
		{"monthly", "6:20.789"},
		{"", "6:10.012"}, // bare (no window typed) means all-time
		{"all-time", "6:10.012"},
	}
	for _, tc := range cases {
		p := newTestProviderPB(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/user", r.URL.Path)
			assert.Equal(t, "Feinberg", r.URL.Query().Get("name"))
			assert.Equal(t, "1", r.URL.Query().Get("sortByTime"))
			_, _ = w.Write([]byte(pbBody))
		}))

		reply := endpoint(t, p, "personal_best")(context.Background(), gossiprpc.Request{Account: "Feinberg", TimeWindow: tc.window}).(gossiprpc.PacemanPersonalBestReply)
		require.Empty(t, reply.Error, "window %q", tc.window)
		assert.False(t, reply.Empty, "window %q", tc.window)
		assert.Equal(t, tc.want, reply.Time, "window %q", tc.window)
	}
}

func TestPersonalBestNoneInWindow(t *testing.T) {
	p := newTestProviderPB(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pbEmptyBody))
	}))

	reply := endpoint(t, p, "personal_best")(context.Background(), gossiprpc.Request{Account: "Newbie", TimeWindow: "daily"}).(gossiprpc.PacemanPersonalBestReply)
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty, "a null pb for the window must read as no personal best, not an error")
	assert.Equal(t, "", reply.Time)
}

func TestPersonalBestUpstream4xx(t *testing.T) {
	p := newTestProviderPB(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`Failed to find user with uuid: UNKNOWN`))
	}))

	reply := endpoint(t, p, "personal_best")(context.Background(), gossiprpc.Request{Account: "ghost"}).(gossiprpc.PacemanPersonalBestReply)
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
