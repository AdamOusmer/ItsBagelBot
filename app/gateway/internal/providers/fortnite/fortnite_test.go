package fortnite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
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
	s.m[key] = append([]byte(nil), val...)
	return nil
}
func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// newTestProvider wires the provider with BOTH upstreams faked: stats is the
// api-fortnite.com double (account lookup + raw stats), shop the
// fortnite-api.com double. extra tweaks the config before building.
func newTestProvider(t *testing.T, stats, shop http.Handler, extra func(*Config)) *Provider {
	t.Helper()
	statsSrv := httptest.NewServer(stats)
	t.Cleanup(statsSrv.Close)
	shopSrv := httptest.NewServer(shop)
	t.Cleanup(shopSrv.Close)
	cfg := Config{ShopBaseURL: shopSrv.URL, StatsBaseURL: statsSrv.URL, APIKey: "fortnite-key"}
	if extra != nil {
		extra(&cfg)
	}
	return New(cfg, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
}

func noUpstream(t *testing.T, name string) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Errorf("no %s upstream call expected", name)
	})
}

func handle(t *testing.T, p *Provider, name string) func(context.Context, gatewayrpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("%s endpoint not declared", name)
	return nil
}

// asReply decodes one handler result (raw wire bytes or typed guard reply).
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

// asStats / asShop are the endpoint-typed instantiations the tests read with.
var (
	asStats    = asReply[gatewayrpc.FortniteStatsReply]
	asShop     = asReply[gatewayrpc.FortniteShopReply]
	asSnapshot = asReply[gatewayrpc.FortniteSnapshotReply]
	asSession  = asReply[gatewayrpc.FortniteSessionReply]
)

// mutableStatsUpstream is statsUpstream with a stats body the test can change
// between calls (to simulate games played between the snapshot and the session
// read); it serves lifetime only, so the season endpoint is never touched.
func mutableStatsUpstream(t *testing.T, account string, body *string, reqs *[]*http.Request) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, r.Clone(context.Background()))
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

// statsUpstream fakes api-fortnite.com: the display-name lookup answers
// account, the stats path answers body, the season endpoint answers a fixed
// 2026-05-30T13:00:00Z begin (epoch 1780146000). Requests are recorded onto
// reqs.
func statsUpstream(t *testing.T, account, body string, reqs *[]*http.Request) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reqs = append(*reqs, r.Clone(context.Background()))
		switch {
		// Lookups match the display name case-insensitively, like Epic's.
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

// syntheticBlob is a hand-checkable raw stats body: two inputs summed on the
// solo pubs playlists, one zero-build duo playlist, one LTM playlist (overall
// only) and noise keys the parser must skip.
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

	reply := asStats(t, handle(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: "ninja"}))
	require.Empty(t, reply.Error)

	// Lookup then stats, both carrying the x-api-key header. The lookup uses
	// the viewer's spelling; the reply carries the canonical one.
	require.Len(t, reqs, 2)
	assert.Equal(t, "/api/v1/account/displayName/ninja", reqs[0].URL.Path)
	for _, r := range reqs {
		assert.Equal(t, "fortnite-key", r.Header.Get("x-api-key"))
	}
	assert.Equal(t, "Ninja", reply.Player)
	assert.Equal(t, "lifetime", reply.Window)

	// Overall spans every playlist (LTM included); the mode buckets span the
	// core pubs playlists only; score/arena/battle-pass noise is skipped.
	assert.Equal(t, int64(20), reply.Overall.Wins)
	assert.Equal(t, int64(150), reply.Overall.Matches)
	assert.Equal(t, int64(450), reply.Overall.Kills)
	assert.Equal(t, int64(12), reply.Solo.Wins)
	assert.Equal(t, int64(100), reply.Solo.Matches)
	assert.Equal(t, int64(300), reply.Solo.Kills)
	assert.Equal(t, int64(5), reply.Duo.Wins)
	assert.Equal(t, int64(45), reply.Duo.Matches)
	assert.Zero(t, reply.Squad.Matches)

	// Derived values: deaths = matches - wins.
	assert.InDelta(t, 300.0/88.0, reply.Solo.KD, 1e-9)
	assert.InDelta(t, 12.0, reply.Solo.WinRate, 1e-9)

	// Second call is served from the reply cache: no new upstream hits.
	_ = asStats(t, handle(t, p, "stats")(context.Background(), gatewayrpc.Request{Account: "Ninja"}))
	assert.Len(t, reqs, 2)
}

// The real 66KB blob the probe captured from api-fortnite.com for Ninja's
// account; expected numbers computed independently from the same fixture.
func TestStatsRealBlobAggregation(t *testing.T) {
	body, err := os.ReadFile("testdata/stats_v2_real.json")
	require.NoError(t, err)
	var resp rawStatsResponse
	require.NoError(t, json.Unmarshal(body, &resp))

	overall, modes := aggregate(resp.Stats)
	assert.Equal(t, modeAgg{wins: 11472, matches: 33287, kills: 221742}, overall)
	assert.Equal(t, modeAgg{wins: 3290, matches: 11645, kills: 82607}, modes[0])
	assert.Equal(t, modeAgg{wins: 3668, matches: 8699, kills: 61606}, modes[1])
	assert.Equal(t, modeAgg{wins: 2954, matches: 7350, kills: 47051}, modes[2])
}

// requestPaths projects the recorded upstream requests to their paths.
func requestPaths(reqs []*http.Request) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.URL.Path)
	}
	return out
}

// The season window auto-resolves its start from the upstream's own season
// endpoint (cached, so a second season lookup hits nothing new) and filters
// the stats call via startTime; lifetime never touches the season endpoint.
func TestStatsSeasonAutoResolved(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"), nil)
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "season", reply.Window)

	reply = asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja", TimeWindow: "lifetime"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "lifetime", reply.Window)

	// lookup + season + season-scoped stats, then just the lifetime stats.
	require.Len(t, reqs, 4, "paths: %v", requestPaths(reqs))
	wantStart := time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC).Unix()
	assert.Equal(t, "/api/v1/season", reqs[1].URL.Path)
	assert.Equal(t, strconv.FormatInt(wantStart, 10), reqs[2].URL.Query().Get("startTime"))
	assert.Empty(t, reqs[3].URL.Query().Get("startTime"))
}

// A manual season-start override skips the upstream season endpoint entirely.
func TestStatsSeasonManualOverride(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, statsUpstream(t, "Ninja", syntheticBlob, &reqs), noUpstream(t, "shop"),
		func(cfg *Config) { cfg.SeasonStartUnix = 1746000000 })

	reply := asStats(t, handle(t, p, "stats")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "season", reply.Window)

	require.Len(t, reqs, 2, "paths: %v", requestPaths(reqs))
	assert.Equal(t, "1746000000", reqs[1].URL.Query().Get("startTime"))
}

// With the season endpoint down the season window degrades to lifetime (and
// says so) instead of failing the command.
func TestStatsSeasonResolveFailureFallsBack(t *testing.T) {
	var reqs []*http.Request
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r.Clone(context.Background()))
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
		gatewayrpc.Request{Account: "Ninja", TimeWindow: "season"}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "lifetime", reply.Window)
	require.Len(t, reqs, 3, "paths: %v", requestPaths(reqs))
	assert.Empty(t, reqs[2].URL.Query().Get("startTime"))
}

// PSN/Xbox lookups are Pro-plan upstream features: friendly error, no
// upstream call. Epic (and blank) pass through.
func TestStatsPlatformNotSupported(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)

	for _, accountType := range []string{"psn", "xbl", "XBL "} {
		reply := asStats(t, handle(t, p, "stats")(context.Background(),
			gatewayrpc.Request{Account: "SomePlayer", AccountType: accountType}))
		assert.Equal(t, "only Epic display names are supported right now", reply.Error)
	}
}

// An unknown name 404s at the lookup with the upstream's wordy passthrough
// body; the reply must chat plain "player not found" and negative-cache.
func TestStatsUnknownPlayerNegativeCached(t *testing.T) {
	var hits int
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"error":"Upstream API error: Response status code does not indicate success: 404 (Not Found)."}`))
	}), noUpstream(t, "shop"), nil)
	h := handle(t, p, "stats")

	reply := asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)

	reply = asStats(t, h(context.Background(), gatewayrpc.Request{Account: "Ghosty"}))
	assert.Equal(t, "player not found", reply.Error)
	assert.Equal(t, 1, hits, "unknown player must be served from the negative cache")
}

func TestStatsMissingAccount(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	reply := asStats(t, handle(t, p, "stats")(context.Background(), gatewayrpc.Request{}))
	assert.Equal(t, "missing account", reply.Error)
}

// A stream-start snapshot records the baseline; after games are played the
// session endpoint reports the lifetime delta with K/D and win rate derived
// over the session's own games.
func TestSessionStartThenDelta(t *testing.T) {
	body := syntheticBlob // overall wins=20, matches=150, kills=450
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)

	snap := asSnapshot(t, handle(t, p, "session_start")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, snap.Error)
	assert.Equal(t, "Ninja", snap.Player)

	// Games played: overall climbs +5 wins, +20 matches, +150 kills.
	body = `{"accountId":"deadbeef","stats":{
		"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 25,
		"br_matchesplayed_keyboardmouse_m0_playlist_defaultsolo": 170,
		"br_kills_keyboardmouse_m0_playlist_defaultsolo": 600
	}}`

	sess := asSession(t, handle(t, p, "session")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, sess.Error)
	assert.True(t, sess.HasSnapshot)
	assert.Equal(t, "Ninja", sess.Player)
	assert.Equal(t, int64(5), sess.Wins)
	assert.Equal(t, int64(20), sess.Matches)
	assert.Equal(t, int64(150), sess.Kills)
	// deaths = 20 - 5 = 15; K/D = 150/15 = 10; win rate = 5*100/20 = 25.
	assert.InDelta(t, 10.0, sess.KD, 1e-9)
	assert.InDelta(t, 25.0, sess.WinRate, 1e-9)
	assert.Positive(t, sess.SinceUnix)
}

// Without a snapshot (module enabled mid-stream) the first session call starts
// tracking and says so; the snapshot it writes gives the next call a baseline.
func TestSessionNoSnapshotStartsTracking(t *testing.T) {
	body := syntheticBlob
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)
	h := handle(t, p, "session")

	sess := asSession(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja", ChannelID: "77"}))
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot)
	assert.Equal(t, "Ninja", sess.Player)

	// The baseline now exists: a follow-up reports a zero delta, not "tracking".
	sess = asSession(t, h(context.Background(), gatewayrpc.Request{Account: "Ninja", ChannelID: "77"}))
	assert.True(t, sess.HasSnapshot)
	assert.Zero(t, sess.Wins)
	assert.Zero(t, sess.Matches)
	assert.Zero(t, sess.Kills)
}

// A snapshot keyed to a different account is not diffed against: the endpoint
// re-snapshots for the new account instead of reporting a bogus delta.
func TestSessionAccountMismatchResnapshots(t *testing.T) {
	body := syntheticBlob
	var reqs []*http.Request
	p := newTestProvider(t, mutableStatsUpstream(t, "Ninja", &body, &reqs), noUpstream(t, "shop"), nil)

	// Seed a snapshot for a different account on this channel.
	require.NoError(t, p.writeSnapshot(context.Background(), "42", "someoneelse", gatewayrpc.FortniteStatsReply{Player: "SomeoneElse"}))

	sess := asSession(t, handle(t, p, "session")(context.Background(),
		gatewayrpc.Request{Account: "Ninja", ChannelID: "42"}))
	require.Empty(t, sess.Error)
	assert.False(t, sess.HasSnapshot, "a foreign-account snapshot must not be diffed against")
}

func TestSessionMissingArgs(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	assert.Equal(t, "missing account or channel",
		asSnapshot(t, handle(t, p, "session_start")(context.Background(), gatewayrpc.Request{Account: "Ninja"})).Error)
	assert.Equal(t, "missing account or channel",
		asSession(t, handle(t, p, "session")(context.Background(), gatewayrpc.Request{ChannelID: "1"})).Error)
}

// Platform lookups the free plan cannot do answer a friendly error on the
// session endpoints too, with no upstream call.
func TestSessionPlatformNotSupported(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), noUpstream(t, "shop"), nil)
	assert.Equal(t, "only Epic display names are supported right now",
		asSnapshot(t, handle(t, p, "session_start")(context.Background(),
			gatewayrpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "psn"})).Error)
	assert.Equal(t, "only Epic display names are supported right now",
		asSession(t, handle(t, p, "session")(context.Background(),
			gatewayrpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "xbl"})).Error)
}

const shopBody = `{
	"status": 200,
	"data": {
		"date": "2026-07-09T00:00:00Z",
		"entries": [
			{"finalPrice": 2800, "bundle": {"name": "Peely Bundle"}, "brItems": [{"name": "Peely"}]},
			{"finalPrice": 1200, "brItems": [{"name": "Renegade Raider"}]},
			{"finalPrice": 500, "tracks": [{"title": "Never Gonna Give You Up"}]},
			{"finalPrice": 400}
		]
	}
}`

func TestShopNormalizesAndCaches(t *testing.T) {
	var hits int
	p := newTestProvider(t, noUpstream(t, "stats"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		require.Equal(t, "/v2/shop", r.URL.Path)
		// The shop upstream is public: the stats key must not leak into it.
		assert.Empty(t, r.Header.Get("x-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(shopBody))
	}), nil)
	h := handle(t, p, "shop")

	reply := asShop(t, h(context.Background(), gatewayrpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, "2026-07-09", reply.Date)
	// The nameless entry is dropped; the bundle keeps its bundle name.
	assert.Equal(t, 3, reply.Count)
	require.Len(t, reply.Entries, 3)
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Peely Bundle", Price: 2800}, reply.Entries[0])
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Renegade Raider", Price: 1200}, reply.Entries[1])
	assert.Equal(t, gatewayrpc.FortniteShopEntry{Name: "Never Gonna Give You Up", Price: 500}, reply.Entries[2])

	// Second call is served from the cache.
	reply = asShop(t, h(context.Background(), gatewayrpc.Request{}))
	require.Empty(t, reply.Error)
	assert.Equal(t, 1, hits)
}

// Keyless (shop-only mode): the stats endpoint is not registered, the shop
// still answers.
func TestKeylessServesShopOnly(t *testing.T) {
	p := newTestProvider(t, noUpstream(t, "stats"), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(shopBody))
	}), func(cfg *Config) { cfg.APIKey = "" })

	names := make([]string, 0, len(p.Endpoints()))
	for _, ep := range p.Endpoints() {
		names = append(names, ep.Name)
	}
	assert.Equal(t, []string{"shop"}, names)

	reply := asShop(t, handle(t, p, "shop")(context.Background(), gatewayrpc.Request{}))
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
	assert.ElementsMatch(t, []string{"shop", "stats", "session_start", "session"}, names)
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
