// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package mcsr_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/mcsr"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProviderWithStore(t testing.TB, handler http.Handler, store *providertest.MemStore) provider.Provider {
	return mcsr.New(mcsr.Config{BaseURL: providertest.Upstream(t, handler)}, providertest.Deps(store))
}

func newProvider(t testing.TB, handler http.Handler) provider.Provider {
	return newProviderWithStore(t, handler, providertest.NewMemStore())
}

func serve(t testing.TB, path string, query url.Values, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, path, r.URL.Path)
		for key := range query {
			assert.Equal(t, query.Get(key), r.URL.Query().Get(key), "query %s", key)
		}
		_, _ = w.Write([]byte(body))
	}
}

func userBody(elo, wins, loses, played int) string {
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

func lastMatchBody(forfeited, decayed bool, winnerUUID string, timeMS int64) string {
	winner := `null`
	if winnerUUID != "" {
		winner = `"` + winnerUUID + `"`
	}
	return `{"status":"success","data":[{
		"date": 1000000000,
		"seedType": "DESERT_TEMPLE",
		"bastionType": "TREASURE",
		"forfeited": ` + strconv.FormatBool(forfeited) + `,
		"decayed": ` + strconv.FormatBool(decayed) + `,
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

const (
	notFoundBody = `{"status":"error","data":null}`
	emptyData    = `{"status":"success","data":[]}`
	versusBody   = `{"status":"success","data":{
		"players": [
			{"uuid":"u-opp","nickname":"lowk3y_"},
			{"uuid":"u-self","nickname":"Feinberg"}
		],
		"results": {
			"ranked": {"total":34,"u-opp":14,"u-self":20},
			"casual": {"total":2,"u-opp":1,"u-self":1}
		}
	}}`
	weeklyRaceBody = `{"status":"success","data":{"id":99,"leaderboard":[
		{"rank":1,"player":{"nickname":"gharfyy"},"time":147374},
		{"rank":2,"player":{"nickname":"Feinberg"},"time":160000}
	]}}`
	eloBoardBody = `{"status":"success","data":{"users":[
		{"nickname":"A","seasonResult":{"eloRate":2400}},
		{"nickname":"B","seasonResult":{"eloRate":2300}}
	]}}`
	phaseBoardBody = `{"status":"success","data":{"users":[
		{"nickname":"A","seasonResult":{"phasePoint":50,"predPhasePoint":80}}
	]}}`
	recordBoardBody = `{"status":"success","data":[
		{"rank":1,"time":395123,"user":{"nickname":"A"}}
	]}`
)

func TestUserReplies(t *testing.T) {
	const unrated = `{"status":"success","data":{"uuid":"u1","nickname":"New","eloRate":null,"eloRank":null,"country":null,"statistics":{"season":{}}}}`
	providertest.RunCases(t, newProvider, "user", []providertest.Case[gossiprpc.McsrUserReply]{
		{Name: "reports the ranked season stats", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: serve(t, "/users/Feinberg", nil, userBody(1650, 40, 20, 61)),
			Want: gossiprpc.McsrUserReply{
				Nickname: "Feinberg", UUID: "u1", Elo: 1650, Rank: 12, Country: "us",
				Wins: 40, Loses: 20, Played: 61, BestTimeMS: 543210,
			}},
		{Name: "reports an unrated player with elo and rank of -1", Req: gossiprpc.Request{Account: "New"},
			Upstream: providertest.Respond(http.StatusOK, unrated),
			Want:     gossiprpc.McsrUserReply{Nickname: "New", UUID: "u1", Elo: -1, Rank: -1}},
		{Name: "answers an unknown player", Req: gossiprpc.Request{Account: "ghost"},
			Upstream: providertest.Respond(http.StatusBadRequest, notFoundBody),
			Want:     gossiprpc.McsrUserReply{Nickname: "ghost", Error: "player not found"}},
		{Name: "asks chat to wait when the API throttles", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Respond(http.StatusTooManyRequests, `{}`),
			Want:     gossiprpc.McsrUserReply{Nickname: "Feinberg", Error: "MCSR Ranked API is busy, try again in a minute"}},
		{Name: "falls back to a generic failure on an upstream outage", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Respond(http.StatusBadGateway, `{}`),
			Want:     gossiprpc.McsrUserReply{Nickname: "Feinberg", Error: "stats lookup failed"}},
		{Name: "rejects a missing account before any upstream call", Req: gossiprpc.Request{},
			Upstream: providertest.Forbid(t),
			Want:     gossiprpc.McsrUserReply{Error: "missing account"}},
	})
}

func TestLastMatchReplies(t *testing.T) {
	win := gossiprpc.McsrLastMatchReply{
		Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "11:03.135",
		Seed: "Desert Temple", Structure: "Treasure", EloChange: 21,
	}
	forfeit := gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "loss", Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, Forfeited: true}
	decayed := gossiprpc.McsrLastMatchReply{Player: "Feinberg", Opponent: "lowk3y_", Result: "win", Time: "8:20.000", Seed: "Desert Temple", Structure: "Treasure", EloChange: 21, Decayed: true}
	matches := func(season string, body string) http.HandlerFunc {
		return serve(t, "/users/Feinberg/matches", url.Values{"count": {"1"}, "season": {season}}, body)
	}

	withoutAge := func(r gossiprpc.McsrLastMatchReply) gossiprpc.McsrLastMatchReply {
		r.AgoSeconds = 0
		return r
	}

	providertest.RunCases(t, newProvider, "last_match", []providertest.Case[gossiprpc.McsrLastMatchReply]{
		{Name: "summarizes a win", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: serve(t, "/users/Feinberg/matches", url.Values{"count": {"1"}}, lastMatchBody(false, false, "u-self", 663135)),
			Want:     win},
		{Name: "forwards the requested season", Req: gossiprpc.Request{Account: "Feinberg", Season: 11},
			Upstream: matches("11", lastMatchBody(false, false, "u-self", 663135)),
			Want:     win},
		{Name: "reads a forfeit as a loss with no completion time", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Respond(http.StatusOK, lastMatchBody(true, false, "u-opp", 0)),
			Want:     forfeit},
		{Name: "flags a decayed match", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Respond(http.StatusOK, lastMatchBody(false, true, "u-self", 500000)),
			Want:     decayed},
		{Name: "reads an empty history as an answer, not an error", Req: gossiprpc.Request{Account: "Newbie"},
			Upstream: providertest.Respond(http.StatusOK, emptyData),
			Want:     gossiprpc.McsrLastMatchReply{Player: "Newbie", Empty: true}},
		{Name: "answers an unknown player", Req: gossiprpc.Request{Account: "ghost"},
			Upstream: providertest.Respond(http.StatusBadRequest, notFoundBody),
			Want:     gossiprpc.McsrLastMatchReply{Player: "ghost", Error: "player not found"}},
	}, withoutAge)
}

func TestVersusReplies(t *testing.T) {
	providertest.RunCases(t, newProvider, "versus", []providertest.Case[gossiprpc.McsrRecordReply]{
		{Name: "counts both players' ranked and casual wins", Req: gossiprpc.Request{Account: "Feinberg", AccountB: "lowk3y_"},
			Upstream: serve(t, "/users/Feinberg/versus/lowk3y_", nil, versusBody),
			Want:     gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "lowk3y_", WinsA: 21, WinsB: 15, Played: 36}},
		{Name: "rejects a missing second account before any upstream call", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Forbid(t),
			Want:     gossiprpc.McsrRecordReply{Error: "missing account"}},
		{Name: "answers an unknown opponent", Req: gossiprpc.Request{Account: "Feinberg", AccountB: "ghost"},
			Upstream: providertest.Respond(http.StatusBadRequest, notFoundBody),
			Want:     gossiprpc.McsrRecordReply{PlayerA: "Feinberg", PlayerB: "ghost", Error: "player not found"}},
	})
}

func TestLeaderboardReplies(t *testing.T) {
	providertest.RunCases(t, newProvider, "leaderboard", []providertest.Case[gossiprpc.McsrLeaderboardReply]{
		{Name: "ranks the elo board for a country", Req: gossiprpc.Request{Country: "us"},
			Upstream: serve(t, "/leaderboard", url.Values{"country": {"us"}}, eloBoardBody),
			Want: gossiprpc.McsrLeaderboardReply{Board: "elo", Entries: []gossiprpc.McsrLeaderboardEntry{
				{Rank: 1, Name: "A", Value: "2400"}, {Rank: 2, Name: "B", Value: "2300"},
			}}},
		{Name: "shows predicted phase points when asked", Req: gossiprpc.Request{Board: "phase", Predicted: true},
			Upstream: serve(t, "/phase-leaderboard", url.Values{"predicted": {"true"}}, phaseBoardBody),
			Want: gossiprpc.McsrLeaderboardReply{Board: "phase", Entries: []gossiprpc.McsrLeaderboardEntry{
				{Rank: 1, Name: "A", Value: "80"},
			}}},
		{Name: "defaults the record board to the current season", Req: gossiprpc.Request{Board: "record"},
			Upstream: serve(t, "/record-leaderboard", url.Values{"season": {"0"}}, recordBoardBody),
			Want: gossiprpc.McsrLeaderboardReply{Board: "record", Entries: []gossiprpc.McsrLeaderboardEntry{
				{Rank: 1, Name: "A", Value: "6:35.123"},
			}}},
		{Name: "reads an empty board as an answer, not an error", Req: gossiprpc.Request{},
			Upstream: providertest.Respond(http.StatusOK, `{"status":"success","data":{"users":[]}}`),
			Want:     gossiprpc.McsrLeaderboardReply{Board: "elo", Entries: []gossiprpc.McsrLeaderboardEntry{}, Empty: true}},
		{Name: "answers an upstream 400", Req: gossiprpc.Request{Board: "phase"},
			Upstream: providertest.Respond(http.StatusBadRequest, notFoundBody),
			Want:     gossiprpc.McsrLeaderboardReply{Board: "phase", Error: "player not found"}},
	})
}

func TestWeeklyRaceReplies(t *testing.T) {
	leaderOnly := gossiprpc.McsrWeeklyRaceReply{Player: "SomeoneElse", LeaderName: "gharfyy", LeaderTime: "2:27.374"}
	boardOf := func(nicknames ...string) string {
		entries := ""
		for i, nickname := range nicknames {
			if i > 0 {
				entries += ","
			}
			entries += `{"rank":` + strconv.Itoa(i+1) + `,"player":{"nickname":"` + nickname + `"},"time":160000}`
		}
		return `{"status":"success","data":{"id":99,"leaderboard":[` + entries + `]}}`
	}

	providertest.RunCases(t, newProvider, "weekly_race", []providertest.Case[gossiprpc.McsrWeeklyRaceReply]{
		{Name: "finds the player on the board", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: serve(t, "/weekly-race", nil, weeklyRaceBody),
			Want: gossiprpc.McsrWeeklyRaceReply{
				Player: "Feinberg", LeaderName: "gharfyy", LeaderTime: "2:27.374",
				HasPlayer: true, PlayerRank: 2, PlayerTime: "2:40.000",
			}},
		{Name: "reports the leader even when the player is not on the board", Req: gossiprpc.Request{Account: "SomeoneElse"},
			Upstream: providertest.Respond(http.StatusOK, weeklyRaceBody),
			Want:     leaderOnly},
		{Name: "reads an empty board as an answer, not an error", Req: gossiprpc.Request{Account: "Feinberg"},
			Upstream: providertest.Respond(http.StatusOK, `{"status":"success","data":{"id":99,"leaderboard":[]}}`),
			Want:     gossiprpc.McsrWeeklyRaceReply{Player: "Feinberg", Empty: true}},
		{Name: "TestASCIIEqualFoldDoesNotFoldUnicode", Req: gossiprpc.Request{Account: "Some"},
			Upstream: providertest.Respond(http.StatusOK, boardOf("ſome")),
			Want:     gossiprpc.McsrWeeklyRaceReply{Player: "Some", LeaderName: "ſome", LeaderTime: "2:40.000"}},
		{Name: "TestASCIIEqualFoldDoesNotFoldUnicode: ASCII case folds", Req: gossiprpc.Request{Account: "nIcKnAmE"},
			Upstream: providertest.Respond(http.StatusOK, boardOf("Nickname")),
			Want: gossiprpc.McsrWeeklyRaceReply{
				Player: "nIcKnAmE", LeaderName: "Nickname", LeaderTime: "2:40.000",
				HasPlayer: true, PlayerRank: 1, PlayerTime: "2:40.000",
			}},
		{Name: "TestASCIIEqualFoldDoesNotFoldUnicode: a length mismatch never matches", Req: gossiprpc.Request{Account: "ab"},
			Upstream: providertest.Respond(http.StatusOK, boardOf("abc")),
			Want:     gossiprpc.McsrWeeklyRaceReply{Player: "ab", LeaderName: "abc", LeaderTime: "2:40.000"}},
	})
}

func TestWeeklyRaceSharesOneUpstreamCallAcrossPlayers(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{Body: weeklyRaceBody})
	p := newProvider(t, upstream)

	for _, account := range []string{"Feinberg", "gharfyy"} {
		_ = providertest.Call[gossiprpc.McsrWeeklyRaceReply](t, p, "weekly_race", gossiprpc.Request{Account: account})
	}

	assert.Equal(t, 1, upstream.Hits())
}

func TestSessionTracksChangesSinceTheStoredBaseline(t *testing.T) {
	var mu sync.Mutex
	elo, wins, loses, played := 1650, 40, 20, 61
	store := providertest.NewMemStore()
	p := newProviderWithStore(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := userBody(elo, wins, loses, played)
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	}), store)
	req := gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}

	start := providertest.Call[gossiprpc.McsrSnapshotReply](t, p, "session_start", req)
	require.Empty(t, start.Error)
	assert.Equal(t, 1650, start.Elo)

	mu.Lock()
	elo, wins, loses, played = 1674, 43, 21, 65
	mu.Unlock()
	require.NoError(t, store.Del(context.Background(), core.Key("mcsr", "user", "feinberg")))

	sess := providertest.Call[gossiprpc.McsrSessionReply](t, p, "session", req)
	require.Empty(t, sess.Error)
	assert.True(t, sess.HasSnapshot)
	assert.Equal(t, 1674, sess.Elo)
	assert.Equal(t, 24, sess.EloChange)
	assert.Equal(t, 3, sess.Wins)
	assert.Equal(t, 1, sess.Loses)
	assert.Equal(t, 4, sess.Played)
}

func TestSessionBaselineBelongsToOneAccount(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startAccount string
		wantSnapshot [2]bool
	}{
		{"the first session read starts tracking", "", [2]bool{false, true}},
		{"a baseline stored for another account is reset", "OldAcc", [2]bool{false, true}},
		{"a baseline stored for the same account is kept", "Feinberg", [2]bool{true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, providertest.Respond(http.StatusOK, userBody(1650, 40, 20, 61)))
			if tc.startAccount != "" {
				start := providertest.Call[gossiprpc.McsrSnapshotReply](t, p, "session_start", gossiprpc.Request{Account: tc.startAccount, ChannelID: "77"})
				require.Empty(t, start.Error)
			}

			for i, want := range tc.wantSnapshot {
				sess := providertest.Call[gossiprpc.McsrSessionReply](t, p, "session", gossiprpc.Request{Account: "Feinberg", ChannelID: "77"})
				require.Empty(t, sess.Error)
				assert.Equal(t, want, sess.HasSnapshot, "read %d", i+1)
				assert.Zero(t, sess.EloChange)
			}
		})
	}
}

func TestSessionEndDropsTheBaseline(t *testing.T) {
	p := newProvider(t, providertest.Respond(http.StatusOK, userBody(1650, 40, 20, 61)))
	req := gossiprpc.Request{Account: "Feinberg", ChannelID: "77"}
	require.Empty(t, providertest.Call[gossiprpc.McsrSnapshotReply](t, p, "session_start", req).Error)

	ended := providertest.Call[gossiprpc.McsrSnapshotReply](t, p, "session_end", req)
	sess := providertest.Call[gossiprpc.McsrSessionReply](t, p, "session", req)

	assert.Empty(t, ended.Error)
	assert.False(t, sess.HasSnapshot, "an ended session must start tracking afresh")
	assert.Equal(t, "missing channel", providertest.Call[gossiprpc.McsrSnapshotReply](t, p, "session_end", gossiprpc.Request{}).Error)
}

func TestSessionRequiresAChannel(t *testing.T) {
	p := newProvider(t, providertest.Forbid(t))

	reply := providertest.Call[gossiprpc.McsrSessionReply](t, p, "session", gossiprpc.Request{Account: "x"})

	assert.Equal(t, "missing account or channel", reply.Error)
}

func TestCacheKeysFoldAccountSeasonAndCountry(t *testing.T) {
	store := providertest.NewMemStore()
	p := newProviderWithStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodies := map[string]string{
			"/users/FrOsTy":                userBody(1650, 40, 20, 61),
			"/users/FrOsTy/matches":        lastMatchBody(false, false, "u-self", 663135),
			"/users/FrOsTy/versus/LowK3y_": versusBody,
			"/leaderboard":                 eloBoardBody,
			"/phase-leaderboard":           phaseBoardBody,
			"/record-leaderboard":          recordBoardBody,
		}
		_, _ = w.Write([]byte(bodies[r.URL.Path]))
	}), store)

	for _, call := range []struct {
		endpoint string
		req      gossiprpc.Request
	}{
		{"user", gossiprpc.Request{Account: "  FrOsTy  "}},
		{"last_match", gossiprpc.Request{Account: "  FrOsTy  ", Season: 3}},
		{"versus", gossiprpc.Request{Account: "  FrOsTy  ", AccountB: " LowK3y_ ", Season: 3}},
		{"leaderboard", gossiprpc.Request{}},
		{"leaderboard", gossiprpc.Request{Season: 2, Country: " Ca "}},
		{"leaderboard", gossiprpc.Request{Board: "phase", Season: 2, Country: "ca", Predicted: true}},
		{"leaderboard", gossiprpc.Request{Board: "record", Season: 4}},
	} {
		_ = providertest.Endpoint(t, p, call.endpoint)(context.Background(), call.req)
	}

	assert.Equal(t, []string{
		"gossip:mcsr:last-match:frosty:3",
		"gossip:mcsr:leaderboard-elo:0:",
		"gossip:mcsr:leaderboard-elo:2:ca",
		"gossip:mcsr:leaderboard-phase:2:ca:predicted",
		"gossip:mcsr:leaderboard-record:4",
		"gossip:mcsr:user:frosty",
		"gossip:mcsr:versus:frosty:3|lowk3y_",
	}, store.Keys())
}
