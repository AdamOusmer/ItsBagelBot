// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mcsr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
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

func (s *memStore) SetNX(_ context.Context, key string, val []byte, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = append([]byte(nil), val...)
	return true, nil
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
func newTestProvider(t *testing.T, handler http.Handler) (provider.Provider, *memStore) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	st := newMemStore()
	return New(Config{BaseURL: srv.URL},
		provider.Deps{Cache: core.NewCache(st), Log: zap.NewNop()}), st
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

// callEndpoint serves handler as the fake MCSR upstream, calls the named
// endpoint once with req and type-asserts the reply to R. This is the
// "stand up an upstream, call one endpoint, unwrap the typed reply" shape
// most tests in this file share; a test that needs the provider or store
// again (a second call, a cache eviction) calls newTestProvider/endpoint
// directly instead.
func callEndpoint[R any](t *testing.T, handler http.Handler, name string, req gossiprpc.Request) R {
	t.Helper()
	p, _ := newTestProvider(t, handler)
	return endpoint(t, p, name)(context.Background(), req).(R)
}

func TestUserParsing(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrUserReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/Feinberg", r.URL.Path)
		_, _ = w.Write([]byte(userBody(1650, 40, 20, 61)))
	}), "user", gossiprpc.Request{Account: "Feinberg"})
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
	reply := callEndpoint[gossiprpc.McsrUserReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}), "user", gossiprpc.Request{Account: "New"})
	require.Empty(t, reply.Error)
	assert.Equal(t, -1, reply.Elo)
	assert.Equal(t, -1, reply.Rank)
}

func TestUserNotFound(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrUserReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // MCSR answers 400 for data not found
		_, _ = w.Write([]byte(`{"status":"error","data":null}`))
	}), "user", gossiprpc.Request{Account: "ghost"})
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

	start := endpoint(t, p, "session_start")(context.Background(), gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}).(gossiprpc.McsrSnapshotReply)
	require.Empty(t, start.Error)
	assert.Equal(t, 1650, start.Elo)

	// The player plays: +24 elo, 3 wins, 1 loss. Evict the 60s user cache so
	// the session read refetches, as it would after the TTL.
	mu.Lock()
	elo, wins, loses, played = 1674, 43, 21, 65
	mu.Unlock()
	require.NoError(t, st.Del(context.Background(), core.Key("mcsr", "user", "feinberg")))

	sess := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}).(gossiprpc.McsrSessionReply)
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

	sess := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}).(gossiprpc.McsrSessionReply)
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot)

	// The call itself planted a snapshot: the next session read has a baseline.
	sess = endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}).(gossiprpc.McsrSessionReply)
	assert.True(t, sess.HasSnapshot)
	assert.Zero(t, sess.EloChange)
}

// A snapshot for another account must not produce a bogus delta.
func TestSessionAccountSwitchResetsBaseline(t *testing.T) {
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(userBody(1650, 40, 20, 61)))
	}))

	start := endpoint(t, p, "session_start")(context.Background(), gossiprpc.Request{Account: "OldAcc", ChannelID: "77"}).(gossiprpc.McsrSnapshotReply)
	require.Empty(t, start.Error)

	sess := endpoint(t, p, "session")(context.Background(), gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}).(gossiprpc.McsrSessionReply)
	assert.False(t, sess.HasSnapshot, "different account must reset the baseline")
}

func TestMissingChannel(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrSessionReply](t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}), "session", gossiprpc.Request{Account: "x"})
	assert.Equal(t, "missing account or channel", reply.Error)
}

// --- last_match ---------------------------------------------------------------

func lastMatchBody(forfeited, decayed bool, winnerUUID string, timeMS int64) string {
	winner := `null`
	if winnerUUID != "" {
		winner = `"` + winnerUUID + `"`
	}
	return `{"status":"success","data":[{
		"date": 1000000000,
		"seedType": "DESERT_TEMPLE",
		"bastionType": "TREASURE",
		"forfeited": ` + boolStr(forfeited) + `,
		"decayed": ` + boolStr(decayed) + `,
		"players": [
			{"uuid":"u-self","nickname":"Feinberg"},
			{"uuid":"u-opp","nickname":"lowk3y_"}
		],
		"result": {"uuid": ` + winner + `, "time": ` + strconv.FormatInt(timeMS, 10) + `},
		"changes": [
			{"uuid":"u-self","change":21},
			{"uuid":"u-opp","change":-21}
		]
	}]}`
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestLastMatchWin(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/Feinberg/matches", r.URL.Path)
		require.Equal(t, "1", r.URL.Query().Get("count"))
		_, _ = w.Write([]byte(lastMatchBody(false, false, "u-self", 663135)))
	}), "last_match", gossiprpc.Request{Account: "Feinberg"})
	require.Empty(t, reply.Error)
	assert.False(t, reply.Empty)
	assert.Equal(t, "Feinberg", reply.Player)
	assert.Equal(t, "lowk3y_", reply.Opponent)
	assert.Equal(t, "win", reply.Result)
	assert.Equal(t, "11:03.135", reply.Time)
	assert.Equal(t, "Desert Temple", reply.Seed)
	assert.Equal(t, "Treasure", reply.Structure)
	assert.Equal(t, 21, reply.EloChange)
	assert.False(t, reply.Forfeited)
	assert.False(t, reply.Decayed)
}

// A forfeit must not read as an ordinary result: the provider still reports
// win/loss from the winner pointer, but Forfeited flags it so the module can
// render it differently instead of implying a clean finish.
func TestLastMatchForfeit(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(lastMatchBody(true, false, "u-opp", 0)))
	}), "last_match", gossiprpc.Request{Account: "Feinberg"})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Forfeited)
	assert.Equal(t, "loss", reply.Result)
	assert.Empty(t, reply.Time, "no completion time on a forfeit before either side finished")
}

func TestLastMatchDecayed(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(lastMatchBody(false, true, "u-self", 500000)))
	}), "last_match", gossiprpc.Request{Account: "Feinberg"})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Decayed)
	assert.Equal(t, "win", reply.Result)
}

func TestLastMatchEmpty(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}), "last_match", gossiprpc.Request{Account: "Newbie"})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

func TestLastMatchNotFound(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","data":null}`))
	}), "last_match", gossiprpc.Request{Account: "ghost"})
	assert.Equal(t, "player not found", reply.Error)
}

func TestLastMatchSeasonForwarded(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLastMatchReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "11", r.URL.Query().Get("season"))
		_, _ = w.Write([]byte(lastMatchBody(false, false, "u-self", 663135)))
	}), "last_match", gossiprpc.Request{Account: "Feinberg", Season: 11})
	require.Empty(t, reply.Error)
}

// --- versus ---------------------------------------------------------------------

func TestVersusParsing(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrRecordReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/Feinberg/versus/lowk3y_", r.URL.Path)
		_, _ = w.Write([]byte(`{"status":"success","data":{
			"players": [
				{"uuid":"u-opp","nickname":"lowk3y_"},
				{"uuid":"u-self","nickname":"Feinberg"}
			],
			"results": {
				"ranked": {"total":34,"u-opp":14,"u-self":20},
				"casual": {"total":2,"u-opp":1,"u-self":1}
			}
		}}`))
	}), "versus", gossiprpc.Request{Account: "Feinberg", AccountB: "lowk3y_"})
	require.Empty(t, reply.Error)
	assert.Equal(t, "Feinberg", reply.PlayerA)
	assert.Equal(t, "lowk3y_", reply.PlayerB)
	assert.Equal(t, 21, reply.WinsA)
	assert.Equal(t, 15, reply.WinsB)
	assert.Equal(t, 36, reply.Played)
}

func TestVersusMissingAccount(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrRecordReply](t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no upstream call expected")
	}), "versus", gossiprpc.Request{Account: "Feinberg"})
	assert.Equal(t, "missing account", reply.Error)
}

func TestVersusNotFound(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrRecordReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","data":null}`))
	}), "versus", gossiprpc.Request{Account: "Feinberg", AccountB: "ghost"})
	assert.Equal(t, "player not found", reply.Error)
}

// --- leaderboard ------------------------------------------------------------------

func TestLeaderboardElo(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLeaderboardReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/leaderboard", r.URL.Path)
		require.Equal(t, "us", r.URL.Query().Get("country"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"users":[
			{"nickname":"A","seasonResult":{"eloRate":2400}},
			{"nickname":"B","seasonResult":{"eloRate":2300}}
		]}}`))
	}), "leaderboard", gossiprpc.Request{Country: "us"})
	require.Empty(t, reply.Error)
	assert.Equal(t, "elo", reply.Board)
	require.Len(t, reply.Entries, 2)
	assert.Equal(t, gossiprpc.McsrLeaderboardEntry{Rank: 1, Name: "A", Value: "2400"}, reply.Entries[0])
}

func TestLeaderboardPhasePredicted(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLeaderboardReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/phase-leaderboard", r.URL.Path)
		require.Equal(t, "true", r.URL.Query().Get("predicted"))
		_, _ = w.Write([]byte(`{"status":"success","data":{"users":[
			{"nickname":"A","seasonResult":{"phasePoint":50,"predPhasePoint":80}}
		]}}`))
	}), "leaderboard", gossiprpc.Request{Board: "phase", Predicted: true})
	require.Empty(t, reply.Error)
	assert.Equal(t, "phase", reply.Board)
	require.Len(t, reply.Entries, 1)
	assert.Equal(t, "80", reply.Entries[0].Value)
}

func TestLeaderboardRecordSeasonDefaultsCurrent(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLeaderboardReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/record-leaderboard", r.URL.Path)
		// Unset Season must still send season=0 explicitly: an omitted param
		// means "all seasons combined" on this one board (see the provider's
		// fetchRecordLeaderboard doc), not "current" like every other board.
		require.Equal(t, "0", r.URL.Query().Get("season"))
		_, _ = w.Write([]byte(`{"status":"success","data":[
			{"rank":1,"time":395123,"user":{"nickname":"A"}}
		]}`))
	}), "leaderboard", gossiprpc.Request{Board: "record"})
	require.Empty(t, reply.Error)
	assert.Equal(t, "record", reply.Board)
	require.Len(t, reply.Entries, 1)
	assert.Equal(t, "6:35.123", reply.Entries[0].Value)
}

func TestLeaderboardEmpty(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLeaderboardReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"users":[]}}`))
	}), "leaderboard", gossiprpc.Request{})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

func TestLeaderboardUpstream400(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrLeaderboardReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","data":null}`))
	}), "leaderboard", gossiprpc.Request{Board: "phase"})
	assert.Equal(t, "player not found", reply.Error)
}

// --- weekly_race ------------------------------------------------------------------

func weeklyRaceBody() string {
	return `{"status":"success","data":{"id":99,"leaderboard":[
		{"rank":1,"player":{"nickname":"gharfyy"},"time":147374},
		{"rank":2,"player":{"nickname":"Feinberg"},"time":160000}
	]}}`
}

func TestWeeklyRaceFindsPlayer(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrWeeklyRaceReply](t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/weekly-race", r.URL.Path)
		_, _ = w.Write([]byte(weeklyRaceBody()))
	}), "weekly_race", gossiprpc.Request{Account: "Feinberg"})
	require.Empty(t, reply.Error)
	assert.Equal(t, "gharfyy", reply.LeaderName)
	assert.Equal(t, "2:27.374", reply.LeaderTime)
	assert.True(t, reply.HasPlayer)
	assert.Equal(t, 2, reply.PlayerRank)
	assert.Equal(t, "2:40.000", reply.PlayerTime)
}

func TestWeeklyRacePlayerNotOnBoard(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrWeeklyRaceReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(weeklyRaceBody()))
	}), "weekly_race", gossiprpc.Request{Account: "SomeoneElse"})
	require.Empty(t, reply.Error)
	assert.False(t, reply.HasPlayer)
	assert.Equal(t, "gharfyy", reply.LeaderName, "leader info is reported even without a player match")
}

func TestWeeklyRaceEmpty(t *testing.T) {
	reply := callEndpoint[gossiprpc.McsrWeeklyRaceReply](t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":99,"leaderboard":[]}}`))
	}), "weekly_race", gossiprpc.Request{Account: "Feinberg"})
	require.Empty(t, reply.Error)
	assert.True(t, reply.Empty)
}

// Two different players in the same week share one cached upstream response:
// the endpoint has no per-player filter, so the provider fetches the whole
// leaderboard once and scans it per request instead of calling twice.
func TestWeeklyRaceSharesOneUpstreamCallAcrossPlayers(t *testing.T) {
	var calls int
	p, _ := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(weeklyRaceBody()))
	}))

	_ = endpoint(t, p, "weekly_race")(context.Background(), gossiprpc.Request{Account: "Feinberg"}).(gossiprpc.McsrWeeklyRaceReply)
	_ = endpoint(t, p, "weekly_race")(context.Background(), gossiprpc.Request{Account: "gharfyy"}).(gossiprpc.McsrWeeklyRaceReply)
	assert.Equal(t, 1, calls)
}

// TestASCIIEqualFoldDoesNotFoldUnicode pins the reason asciiEqualFold exists
// rather than strings.EqualFold. EqualFold applies Unicode simple folding, so
// it reports "ſome" (long s) equal to "Some" — a non-ASCII account would
// then take an unrelated player's rank and time off the weekly board. The
// previous strings.ToLower comparison did not do that, and neither does this.
func TestASCIIEqualFoldDoesNotFoldUnicode(t *testing.T) {
	if !asciiEqualFold("Nickname", "nIcKnAmE") {
		t.Fatal("ASCII case must fold")
	}
	if asciiEqualFold("ſome", "Some") {
		t.Fatal("long s must not fold to ASCII s")
	}
	if asciiEqualFold("abc", "ab") {
		t.Fatal("length mismatch must not match")
	}
}
