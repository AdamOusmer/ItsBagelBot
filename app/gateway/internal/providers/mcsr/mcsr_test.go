package mcsr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func userBody(elo int, wins, loses, played int) string {
	i := strconv.Itoa
	return `{
		"status": "success",
		"data": {
			"uuid": "u1", "nickname": "Feinberg",
			"eloRate": ` + i(elo) + `, "eloRank": 12, "country": "us",
			"statistics": {
				"season": {
					"wins": {"ranked": ` + i(wins) + `, "casual": 1},
					"loses": {"ranked": ` + i(loses) + `, "casual": 0},
					"playedMatches": {"ranked": ` + i(played) + `, "casual": 1},
					"bestTime": {"ranked": 543210, "casual": null}
				}
			}
		}
	}`
}

// newTestProvider serves handler as the MCSR API and returns the provider plus
// its backing store (so tests can evict the 60s user cache to simulate time).
func newTestProvider(t *testing.T, handler http.Handler) (*Provider, *memStore) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	st := newMemStore()
	return New(Config{BaseURL: srv.URL}, core.NewCache(st), nil, zap.NewNop()), st
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

func TestUserParsing(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/Feinberg", r.URL.Path)
		_, _ = w.Write([]byte(userBody(1650, 40, 20, 61)))
	}))

	reply := endpoint(t, p, "user")(context.Background(), gatewayrpc.Request{Account: "Feinberg"}).(gatewayrpc.McsrUserReply)
	require.Empty(t, reply.Error)
	assert.Equal(t, "Feinberg", reply.Nickname)
	assert.Equal(t, 1650, reply.Elo)
	assert.Equal(t, 12, reply.Rank)
	assert.Equal(t, 40, reply.Wins)
	assert.Equal(t, 20, reply.Loses)
	assert.Equal(t, 61, reply.Played)
	assert.Equal(t, int64(543210), reply.BestTimeMS)
}

func TestUserUnrated(t *testing.T) {
	body := `{"status":"success","data":{"uuid":"u1","nickname":"New","eloRate":null,"eloRank":null,"country":null,"statistics":{"season":{}}}}`
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))

	reply := endpoint(t, p, "user")(context.Background(), gatewayrpc.Request{Account: "New"}).(gatewayrpc.McsrUserReply)
	require.Empty(t, reply.Error)
	assert.Equal(t, -1, reply.Elo)
	assert.Equal(t, -1, reply.Rank)
}

func TestUserNotFound(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // MCSR answers 400 for data not found
		_, _ = w.Write([]byte(`{"status":"error","data":null}`))
	}))

	reply := endpoint(t, p, "user")(context.Background(), gatewayrpc.Request{Account: "ghost"}).(gatewayrpc.McsrUserReply)
	assert.Equal(t, "player not found", reply.Error)
}

// TestSessionFlow drives the full snapshot lifecycle: session_start stores the
// baseline, the player wins games, session reports the delta.
func TestSessionFlow(t *testing.T) {
	var mu sync.Mutex
	elo, wins, loses, played := 1650, 40, 20, 61
	p, st := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := userBody(elo, wins, loses, played)
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))

	start := endpoint(t, p, "session_start")(context.Background(), gatewayrpc.Request{Account: "Feinberg", ChannelID: "77"}).(gatewayrpc.McsrSnapshotReply)
	require.Empty(t, start.Error)
	assert.Equal(t, 1650, start.Elo)

	// The player plays: +24 elo, 3 wins, 1 loss. Evict the 60s user cache so
	// the session read refetches, as it would after the TTL.
	mu.Lock()
	elo, wins, loses, played = 1674, 43, 21, 65
	mu.Unlock()
	require.NoError(t, st.Del(context.Background(), core.Key("mcsr", "user", "feinberg")))

	sess := endpoint(t, p, "session")(context.Background(), gatewayrpc.Request{Account: "Feinberg", ChannelID: "77"}).(gatewayrpc.McsrSessionReply)
	require.Empty(t, sess.Error)
	assert.True(t, sess.HasSnapshot)
	assert.Equal(t, 1674, sess.Elo)
	assert.Equal(t, 24, sess.EloChange)
	assert.Equal(t, 3, sess.Wins)
	assert.Equal(t, 1, sess.Loses)
	assert.Equal(t, 4, sess.Played)
}

func TestSessionWithoutSnapshotStartsTracking(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(userBody(1650, 40, 20, 61)))
	}))

	sess := endpoint(t, p, "session")(context.Background(), gatewayrpc.Request{Account: "Feinberg", ChannelID: "77"}).(gatewayrpc.McsrSessionReply)
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot)

	// The call itself planted a snapshot: the next session read has a baseline.
	sess = endpoint(t, p, "session")(context.Background(), gatewayrpc.Request{Account: "Feinberg", ChannelID: "77"}).(gatewayrpc.McsrSessionReply)
	assert.True(t, sess.HasSnapshot)
	assert.Zero(t, sess.EloChange)
}

// A snapshot for another account must not produce a bogus delta.
func TestSessionAccountSwitchResetsBaseline(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(userBody(1650, 40, 20, 61)))
	}))

	start := endpoint(t, p, "session_start")(context.Background(), gatewayrpc.Request{Account: "OldAcc", ChannelID: "77"}).(gatewayrpc.McsrSnapshotReply)
	require.Empty(t, start.Error)

	sess := endpoint(t, p, "session")(context.Background(), gatewayrpc.Request{Account: "Feinberg", ChannelID: "77"}).(gatewayrpc.McsrSessionReply)
	assert.False(t, sess.HasSnapshot, "different account must reset the baseline")
}

func TestMissingChannel(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}))
	reply := endpoint(t, p, "session")(context.Background(), gatewayrpc.Request{Account: "x"}).(gatewayrpc.McsrSessionReply)
	assert.Equal(t, "missing account or channel", reply.Error)
}
