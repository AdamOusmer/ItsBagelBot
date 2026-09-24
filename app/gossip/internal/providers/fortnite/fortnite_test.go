// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

func newTestProvider(t *testing.T, stats, shop http.Handler, extra func(*Config)) *api {
	t.Helper()
	p, _ := newTestProviderWithStore(t, stats, shop, extra)
	return p
}

func newTestProviderWithStore(t *testing.T, stats, shop http.Handler, extra func(*Config)) (*api, *memStore) {
	t.Helper()
	statsSrv := httptest.NewServer(stats)
	t.Cleanup(statsSrv.Close)
	shopSrv := httptest.NewServer(shop)
	t.Cleanup(shopSrv.Close)
	cfg := Config{ShopBaseURL: shopSrv.URL, StatsBaseURL: statsSrv.URL, APIKey: "fortnite-key"}
	if extra != nil {
		extra(&cfg)
	}
	store := newMemStore()
	d := provider.Deps{Cache: core.NewCache(store), Log: zap.NewNop()}
	bldr := provider.NewProvider(providerName, d).Trusted()
	return newAPI(cfg, d, bldr), store
}

func noUpstream(t *testing.T, name string) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Errorf("no %s upstream call expected", name)
	})
}

func handle(t *testing.T, p *api, name string) func(context.Context, gossiprpc.Request) any {
	t.Helper()
	for _, ep := range p.build().Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("%s endpoint not declared", name)
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

var (
	asStats    = asReply[gossiprpc.FortniteStatsReply]
	asShop     = asReply[gossiprpc.FortniteShopReply]
	asSnapshot = asReply[gossiprpc.FortniteSnapshotReply]
	asSession  = asReply[gossiprpc.FortniteSessionReply]
)

func mutableStatsUpstream(t *testing.T, account string, body *string, reqs *[]*http.Request) http.Handler {
	t.Helper()
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*reqs = append(*reqs, r.Clone(context.Background()))
		mu.Unlock()
		switch {
		case strings.EqualFold(r.URL.Path, "/api/v1/account/displayName/"+account):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"` + account + `"}`))
		case r.URL.Path == "/api/v2/stats/deadbeef":
			_, _ = w.Write([]byte(*body))
		default:
			t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func statsUpstream(t *testing.T, account, body string, reqs *[]*http.Request) http.Handler {
	t.Helper()
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*reqs = append(*reqs, r.Clone(context.Background()))
		mu.Unlock()
		switch {
		case strings.EqualFold(r.URL.Path, "/api/v1/account/displayName/"+account):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"` + account + `"}`))
		case r.URL.Path == "/api/v2/stats/deadbeef":
			_, _ = w.Write([]byte(body))
		case r.URL.Path == "/api/v1/season":
			_, _ = w.Write([]byte(`{"seasonDateBegin":"2026-05-30T13:00:00Z","seasonDateEnd":"2026-08-21T13:00:00Z","seasonNumber":41}`))
		default:
			t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

const syntheticBlob = `{
	"accountId": "deadbeef",
	"stats": {
		"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 10,
		"br_placetop1_gamepad_m0_playlist_defaultsolo": 2,
		"br_matchesplayed_keyboardmouse_m0_playlist_defaultsolo": 90,
		"br_matchesplayed_gamepad_m0_playlist_defaultsolo": 10,
		"br_kills_keyboardmouse_m0_playlist_defaultsolo": 300,
		"br_placetop1_keyboardmouse_m0_playlist_nobuildbr_duo": 5,
		"br_matchesplayed_keyboardmouse_m0_playlist_nobuildbr_duo": 45,
		"br_kills_keyboardmouse_m0_playlist_nobuildbr_duo": 100,
		"br_placetop1_keyboardmouse_m0_playlist_gungame_reverse": 3,
		"br_matchesplayed_keyboardmouse_m0_playlist_gungame_reverse": 5,
		"br_kills_keyboardmouse_m0_playlist_gungame_reverse": 50,
		"br_score_keyboardmouse_m0_playlist_defaultsolo": 9999,
		"br_arena_matchesplayed_keyboardmouse_m0_playlist_nobuildbr_habanero_solo": 77,
		"s29_social_bp_level": 414
	}
}`

func TestStatsResolvesAndAggregates(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"), nil)

	reply := asStats(t, handle(t, p, "stats")(context.Background(), gossiprpc.Request{Account: "ninja"}))
	require.Empty(t, reply.Error)

	require.Len(t, reqs, 2)
	assert.Equal(t, "/api/v1/account/displayName/ninja", reqs[0].URL.Path)
	for _, r := range reqs {
		assert.Equal(t, "fortnite-key", r.Header.Get("x-api-key"))
	}
	assert.Equal(t, "Ninja", reply.Player)
	assert.Equal(t, "lifetime", reply.Window)

	assert.Equal(t, int64(20), reply.Overall.Wins)
	assert.Equal(t, int64(150), reply.Overall.Matches)
	assert.Equal(t, int64(450), reply.Overall.Kills)
	assert.Equal(t, int64(12), reply.Solo.Wins)
	assert.Equal(t, int64(100), reply.Solo.Matches)
	assert.Equal(t, int64(300), reply.Solo.Kills)
	assert.Equal(t, int64(5), reply.Duo.Wins)
	assert.Equal(t, int64(45), reply.Duo.Matches)
	assert.Zero(t, reply.Squad.Matches)

	assert.InDelta(t, 300.0/88.0, reply.Solo.KD, 1e-9)
	assert.InDelta(t, 12.0, reply.Solo.WinRate, 1e-9)

	_ = asStats(t, handle(t, p, "stats")(context.Background(), gossiprpc.Request{Account: "Ninja"}))
	assert.Len(t, reqs, 2)
}

func (a *modeAgg) add(metric string, v int64) {
	switch metric {
	case "placetop1":
		a.wins += v
	case "kills":
		a.kills += v
	case "matchesplayed":
		a.matches += v
	}
}

func TestStatsRealBlobAggregation(t *testing.T) {
	body, err := os.ReadFile("testdata/stats_v2_real.json")
	require.NoError(t, err)
	var resp rawStatsResponse
	require.NoError(t, codec.Unmarshal(body, &resp))

	wantOverall := modeAgg{wins: 11472, matches: 33287, kills: 221742}
	wantModes := [3]modeAgg{
		{wins: 3290, matches: 11645, kills: 82607},
		{wins: 3668, matches: 8699, kills: 61606},
		{wins: 2954, matches: 7350, kills: 47051},
	}

	assert.Equal(t, wantOverall, resp.Overall)
	assert.Equal(t, wantModes[0], resp.Modes[0])
	assert.Equal(t, wantModes[1], resp.Modes[1])
	assert.Equal(t, wantModes[2], resp.Modes[2])

	var mapResp struct {
		Stats map[string]float64 `json:"stats"`
	}
	require.NoError(t, codec.Unmarshal(body, &mapResp))
	mapOverall, mapModes := aggregate(mapResp.Stats)
	assert.Equal(t, wantOverall, mapOverall)
	assert.Equal(t, wantModes, mapModes)
}

func requestPaths(reqs []*http.Request) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.URL.Path)
	}
	return out
}

func TestStatsSeasonAutoResolved(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"), nil)
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gossiprpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "season", reply.Window)

	reply = asStats(t, h(context.Background(), gossiprpc.Request{Account: "Ninja", TimeWindow: "lifetime"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "lifetime", reply.Window)

	require.Len(t, reqs, 4, "paths: %v", requestPaths(reqs))
	wantStart := time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC).Unix()
	firstTwo := []string{reqs[0].URL.Path, reqs[1].URL.Path}
	assert.Contains(t, firstTwo, "/api/v1/season")
	assert.Contains(t, firstTwo, "/api/v1/account/displayName/Ninja")
	assert.Equal(t, strconv.FormatInt(wantStart, 10), reqs[2].URL.Query().Get("startTime"))
	assert.Empty(t, reqs[3].URL.Query().Get("startTime"))
}

func TestStatsSeasonManualOverride(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"),
		func(cfg *Config) { cfg.SeasonStartUnix = 1746000000 })

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "season", reply.Window)

	require.Len(t, reqs, 2, "paths: %v", requestPaths(reqs))
	assert.Equal(t, "1746000000", reqs[1].URL.Query().Get("startTime"))
}

func TestStatsSeasonResolveFailureFallsBack(t *testing.T) {
	var mu sync.Mutex
	var reqs []*http.Request
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Clone(context.Background()))
		mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/account/displayName/"):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"Ninja"}`))
		case r.URL.Path == "/api/v2/stats/deadbeef":
			_, _ = w.Write([]byte(syntheticBlob))
		case r.URL.Path == "/api/v1/season":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":500,"error":"An unexpected error occurred"}`))
		}
	}), noUpstream(t, "shop"), nil)

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "lifetime", reply.Window)
	require.Len(t, reqs, 3, "paths: %v", requestPaths(reqs))
	assert.Empty(t, reqs[2].URL.Query().Get("startTime"))
}

type seasonRaceUpstream struct {
	seasonCalls   int32
	seasonStarted chan struct{}
	accountFailed chan struct{}
	seasonCtxErr  error
}

func newSeasonRaceUpstream() *seasonRaceUpstream {
	return &seasonRaceUpstream{
		seasonStarted: make(chan struct{}),
		accountFailed: make(chan struct{}),
	}
}

func (u *seasonRaceUpstream) calls() int32 {
	return atomic.LoadInt32(&u.seasonCalls)
}

func (u *seasonRaceUpstream) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/season":
			u.serveSeason(t, w, r)
		case r.URL.Path == "/api/v1/account/displayName/Ghosty":
			u.serveFailingAccount(t, w)
		case strings.EqualFold(r.URL.Path, "/api/v1/account/displayName/Ninja"):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"Ninja"}`))
		case r.URL.Path == "/api/v2/stats/deadbeef":
			_, _ = w.Write([]byte(syntheticBlob))
		default:
			t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
		}
	})
}

func (u *seasonRaceUpstream) serveSeason(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if atomic.AddInt32(&u.seasonCalls, 1) == 1 {
		close(u.seasonStarted)
		select {
		case <-u.accountFailed:
		case <-time.After(2 * time.Second):
			t.Error("account leg never resolved")
		}
		u.seasonCtxErr = r.Context().Err()
	}
	_, _ = w.Write([]byte(`{"seasonDateBegin":"2026-05-30T13:00:00Z","seasonDateEnd":"2026-08-21T13:00:00Z","seasonNumber":41}`))
}

func (u *seasonRaceUpstream) serveFailingAccount(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	select {
	case <-u.seasonStarted:
	case <-time.After(2 * time.Second):
		t.Error("season fetch never started before the account leg resolved")
	}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"status":404,"error":"Upstream API error: not found"}`))
	close(u.accountFailed)
}

func TestStatsSeasonSurvivesAccountFailure(t *testing.T) {
	race := newSeasonRaceUpstream()
	p := newTestProvider(t, race.handler(t), noUpstream(t, "shop"), nil)

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ghosty", TimeWindow: "season"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.NoError(t, race.seasonCtxErr, "the account leg's failure must not cancel the shared season fetch")

	reply2 := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply2.Error)
	assert.Equal(t, "season", reply2.Window)
	assert.Equal(t, int32(1), race.calls(), "season endpoint must be hit once, cached thereafter")
}

func TestStatsSeasonOverrideSkipsConcurrentPath(t *testing.T) {
	var reqs []*http.Request
	stats := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Clone(context.Background()))
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/account/displayName/"):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"Ninja"}`))
		case r.URL.Path == "/api/v2/stats/deadbeef":
			_, _ = w.Write([]byte(syntheticBlob))
		case r.URL.Path == "/api/v1/season":
			t.Error("season endpoint must not be called when the override is set")
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	p := newTestProvider(t, stats, noUpstream(t, "shop"),
		func(cfg *Config) { cfg.SeasonStartUnix = 1746000000 })

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gossiprpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "season", reply.Window)
	require.Len(t, reqs, 2, "paths: %v", requestPaths(reqs))
	assert.Equal(t, "1746000000", reqs[1].URL.Query().Get("startTime"))
}

func TestStatsPlatformNotSupported(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)

	for _, accountType := range []string{"psn", "xbl", "XBL "} {
		reply := asStats(t, handle(t, p, "stats")(context.Background(),
			gossiprpc.Request{Account: "SomePlayer", AccountType: accountType}))
		assert.Equal(t, "only Epic display names are supported right now", reply.Error)
	}
}

func TestStatsUnknownPlayerNegativeCached(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"error":"Upstream API error: Response status code does not indicate success: 404 (Not Found)."}`))
	}), noUpstream(t, "shop"), nil)
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gossiprpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)

	reply = asStats(t, h(context.Background(), gossiprpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.Equal(t, 1, hits, "unknown player must be served from the negative cache")
}

func TestStatsMissingAccount(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	reply := asStats(t, handle(t, p, "stats")(context.Background(), gossiprpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

func TestSessionStartThenDelta(t *testing.T) {
	body := syntheticBlob
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)

	snap := asSnapshot(t, handle(t, p, "session_start")(context.Background(),
		gossiprpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, snap.Error)
	assert.Equal(t, "Ninja", snap.Player)

	body = `{"accountId":"deadbeef","stats":{
		"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 25,
		"br_matchesplayed_keyboardmouse_m0_playlist_defaultsolo": 170,
		"br_kills_keyboardmouse_m0_playlist_defaultsolo": 600
	}}`

	sess := asSession(t, handle(t, p, "session")(context.Background(),
		gossiprpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, sess.Error)
	assert.True(t, sess.HasSnapshot)
	assert.Equal(t, "Ninja", sess.Player)
	assert.Equal(t, int64(5), sess.Wins)
	assert.Equal(t, int64(20), sess.Matches)
	assert.Equal(t, int64(150), sess.Kills)
	assert.InDelta(t, 10.0, sess.KD, 1e-9)
	assert.InDelta(t, 25.0, sess.WinRate, 1e-9)
	assert.Positive(t, sess.SinceUnix)
}

func TestSessionNoSnapshotStartsTracking(t *testing.T) {
	body := syntheticBlob
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)
	h := handle(t, p, "session")

	sess := asSession(t, h(context.Background(), gossiprpc.Request{Account: "Ninja", ChannelID: "77"}))
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot)
	assert.Equal(t, "Ninja", sess.Player)

	sess = asSession(t, h(context.Background(), gossiprpc.Request{Account: "Ninja", ChannelID: "77"}))
	assert.True(t, sess.HasSnapshot)
	assert.Zero(t, sess.Wins)
	assert.Zero(t, sess.Matches)
	assert.Zero(t, sess.Kills)
}

func TestSessionAccountMismatchResnapshots(t *testing.T) {
	body := syntheticBlob
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)

	require.NoError(t, p.writeSnapshot(context.Background(), "42", "someoneelse", gossiprpc.FortniteStatsReply{Player: "SomeoneElse"}))

	sess := asSession(t, handle(t, p, "session")(context.Background(),
		gossiprpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot, "a foreign-account snapshot must not be diffed against")
}

func TestSessionMissingArgs(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	assert.Equal(t, "missing account or channel",
		asSnapshot(t, handle(t, p, "session_start")(context.Background(), gossiprpc.Request{Account: "Ninja"})).Error)
	assert.Equal(t, "missing account or channel",
		asSession(t, handle(t, p, "session")(context.Background(), gossiprpc.Request{ChannelID: "1"})).Error)
}

func TestSessionPlatformNotSupported(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	assert.Equal(t, "only Epic display names are supported right now",
		asSnapshot(t, handle(t, p, "session_start")(context.Background(),
			gossiprpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "psn"})).Error)
	assert.Equal(t, "only Epic display names are supported right now",
		asSession(t, handle(t, p, "session")(context.Background(),
			gossiprpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "xbl"})).Error)
}

func TestKeylessServesShopOnly(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(shopBody))
	}), func(cfg *Config) { cfg.APIKey = "" })

	built := p.build()
	names := make([]string, 0, len(built.Endpoints()))
	for _, ep := range built.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Equal(t, []string{"shop"}, names)

	reply := asShop(t, handle(t, p, "shop")(context.Background(), gossiprpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 3, reply.Count)
}

func TestKeyedServesAllEndpoints(t *testing.T) {
	p := New(Config{APIKey: "k"},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	names := make([]string, 0, len(p.Endpoints()))
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.ElementsMatch(t, []string{"shop", "stats", "session_start", "session", "session_end"}, names)
}

func TestOddRateLimitDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		New(Config{APIKey: "k", ShopRateLimit: 100.3, StatsRateLimit: 41.7},
			provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	})
}

func TestEpicOnly(t *testing.T) {
	assert.Empty(t, epicOnly(""))
	assert.Empty(t, epicOnly(" Epic "))
	assert.NotEmpty(t, epicOnly("psn"))
	assert.NotEmpty(t, epicOnly("steam"))
}

func BenchmarkStatsZeroAllocUnmarshal(b *testing.B) {
	data, err := os.ReadFile("testdata/stats_v2_real.json")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var resp rawStatsResponse
		if err := resp.UnmarshalJSON(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStatsLegacyMapAggregate(b *testing.B) {
	data, err := os.ReadFile("testdata/stats_v2_real.json")
	if err != nil {
		b.Fatal(err)
	}
	var mapResp struct {
		Stats map[string]float64 `json:"stats"`
	}
	if err := codec.Unmarshal(data, &mapResp); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = aggregate(mapResp.Stats)
	}
}

func TestStatsCacheIDBytes(t *testing.T) {
	cases := []struct {
		window  string
		account string
		want    string
	}{
		{window: "season", account: "Ninja", want: "season:ninja"},
		{window: " SeAsOn ", account: "  NiNjA  ", want: "season:ninja"},
		{window: "", account: "ninja", want: "lifetime:ninja"},
		{window: "career", account: "ninja", want: "lifetime:ninja"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, statsCacheID(c.window, c.account), "window %q account %q", c.window, c.account)
	}
}
