// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package fortnite

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	displayNamePath = "/api/v1/account/displayName/"
	statsLookupPath = "/api/v2/stats/deadbeef"
	seasonPath      = "/api/v1/season"
	seasonBody      = `{"seasonDateBegin":"2026-05-30T13:00:00Z","seasonDateEnd":"2026-08-21T13:00:00Z","seasonNumber":41}`
	epicOnlyError   = "only Epic display names are supported right now"
)

func newProviderWithStore(t testing.TB, stats, shop http.Handler, mutate ...func(*Config)) (provider.Provider, *providertest.MemStore) {
	cfg := Config{
		ShopBaseURL:  providertest.Upstream(t, shop),
		StatsBaseURL: providertest.Upstream(t, stats),
		APIKey:       "fortnite-key",
	}
	for _, fn := range mutate {
		fn(&cfg)
	}
	store := providertest.NewMemStore()
	return New(cfg, providertest.Deps(store)), store
}

func newTestProvider(t testing.TB, stats, shop http.Handler, mutate ...func(*Config)) provider.Provider {
	p, _ := newProviderWithStore(t, stats, shop, mutate...)
	return p
}

type statsUpstream struct {
	t        testing.TB
	accounts []string

	mu           sync.Mutex
	body         string
	seasonStatus int
	reqs         []*http.Request
}

func newStatsUpstream(t testing.TB, account, body string) *statsUpstream {
	return &statsUpstream{t: t, accounts: []string{account}, body: body}
}

func (u *statsUpstream) alsoKnows(accounts ...string) *statsUpstream {
	u.accounts = append(u.accounts, accounts...)
	return u
}

func (u *statsUpstream) canonicalName(path string) (string, bool) {
	for _, name := range u.accounts {
		if strings.EqualFold(path, displayNamePath+name) {
			return name, true
		}
	}
	return "", false
}

func (u *statsUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.reqs = append(u.reqs, r.Clone(context.Background()))
	body, seasonStatus := u.body, u.seasonStatus
	u.mu.Unlock()

	name, known := u.canonicalName(r.URL.Path)
	switch {
	case known:
		fmt.Fprintf(w, `{"id":"deadbeef","displayName":%q}`, name)
	case r.URL.Path == statsLookupPath:
		_, _ = w.Write([]byte(body))
	case r.URL.Path == seasonPath && seasonStatus != 0:
		w.WriteHeader(seasonStatus)
		_, _ = w.Write([]byte(`{"status":500,"error":"An unexpected error occurred"}`))
	case r.URL.Path == seasonPath:
		_, _ = w.Write([]byte(seasonBody))
	default:
		u.t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *statsUpstream) setBody(body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.body = body
}

func (u *statsUpstream) paths() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, 0, len(u.reqs))
	for _, r := range u.reqs {
		out = append(out, r.URL.Path)
	}
	return out
}

func (u *statsUpstream) statsStartTime() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	for i := len(u.reqs) - 1; i >= 0; i-- {
		if u.reqs[i].URL.Path == statsLookupPath {
			return u.reqs[i].URL.Query().Get("startTime")
		}
	}
	return ""
}

func (u *statsUpstream) header(name string) []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]string, 0, len(u.reqs))
	for _, r := range u.reqs {
		out = append(out, r.Header.Get(name))
	}
	return out
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
	up := newStatsUpstream(t, "Ninja", syntheticBlob)
	p := newTestProvider(t, up, providertest.Forbid(t))

	reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "ninja"})
	require.Empty(t, reply.Error)

	assert.Equal(t, []string{displayNamePath + "ninja", statsLookupPath}, up.paths())
	assert.Equal(t, []string{"fortnite-key", "fortnite-key"}, up.header("x-api-key"))
	assert.Equal(t, "Ninja", reply.Player)
	assert.Equal(t, "lifetime", reply.Window)
	assert.Equal(t, gossiprpc.FortniteModeStats{Wins: 20, Matches: 150, Kills: 450}, withoutRatios(reply.Overall))
	assert.Equal(t, gossiprpc.FortniteModeStats{Wins: 12, Matches: 100, Kills: 300}, withoutRatios(reply.Solo))
	assert.Equal(t, gossiprpc.FortniteModeStats{Wins: 5, Matches: 45, Kills: 100}, withoutRatios(reply.Duo))
	assert.Zero(t, reply.Squad.Matches)
	assert.InDelta(t, 300.0/88.0, reply.Solo.KD, 1e-9)
	assert.InDelta(t, 12.0, reply.Solo.WinRate, 1e-9)

	_ = providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja"})
	assert.Len(t, up.paths(), 2, "a repeat lookup is served from the cache")
}

func withoutRatios(m gossiprpc.FortniteModeStats) gossiprpc.FortniteModeStats {
	m.KD, m.WinRate = 0, 0
	return m
}

func TestStatsSeasonWindow(t *testing.T) {
	seasonStart := strconv.FormatInt(time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC).Unix(), 10)
	for _, tc := range []struct {
		name           string
		window         string
		seasonStatus   int
		configuredBase int64
		wantWindow     string
		wantStartTime  string
		wantSeasonCall bool
	}{
		{"resolves the season start from the upstream", "season", 0, 0, "season", seasonStart, true},
		{"falls back to lifetime when the season lookup fails", "season", http.StatusInternalServerError, 0, "lifetime", "", true},
		{"a lifetime request never asks for the season", "lifetime", 0, 0, "lifetime", "", false},
		{"a configured season start skips the lookup", "season", 0, 1746000000, "season", "1746000000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newStatsUpstream(t, "Ninja", syntheticBlob)
			up.seasonStatus = tc.seasonStatus
			p := newTestProvider(t, up, providertest.Forbid(t), func(cfg *Config) { cfg.SeasonStartUnix = tc.configuredBase })

			reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja", TimeWindow: tc.window})

			require.Empty(t, reply.Error)
			assert.Equal(t, tc.wantWindow, reply.Window)
			assert.Equal(t, tc.wantStartTime, up.statsStartTime())
			assert.Equal(t, tc.wantSeasonCall, slices.Contains(up.paths(), seasonPath))
		})
	}
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
		case r.URL.Path == seasonPath:
			u.serveSeason(t, w, r)
		case r.URL.Path == displayNamePath+"Ghosty":
			u.serveFailingAccount(t, w)
		case strings.EqualFold(r.URL.Path, displayNamePath+"Ninja"):
			_, _ = w.Write([]byte(`{"id":"deadbeef","displayName":"Ninja"}`))
		case r.URL.Path == statsLookupPath:
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
	_, _ = w.Write([]byte(seasonBody))
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
	p := newTestProvider(t, race.handler(t), providertest.Forbid(t))

	reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ghosty", TimeWindow: "season"})
	assert.Equal(t, "player not found", reply.Error)
	assert.NoError(t, race.seasonCtxErr, "the account leg's failure must not cancel the shared season fetch")

	reply2 := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja", TimeWindow: "season"})
	require.Empty(t, reply2.Error)
	assert.Equal(t, "season", reply2.Window)
	assert.Equal(t, int32(1), race.calls(), "season endpoint must be hit once, cached thereafter")
}

type slowSeasonUpstream struct {
	seasonEnded chan error
}

func newSlowSeasonUpstream() *slowSeasonUpstream {
	return &slowSeasonUpstream{seasonEnded: make(chan error, 1)}
}

func (u *slowSeasonUpstream) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case seasonPath:
			u.parkUntilHangup(t, r)
		case displayNamePath + "Ghosty":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"error":"Upstream API error: not found"}`))
		default:
			t.Errorf("unexpected stats-upstream path %s", r.URL.Path)
		}
	})
}

func (u *slowSeasonUpstream) parkUntilHangup(t *testing.T, r *http.Request) {
	t.Helper()
	select {
	case <-r.Context().Done():
		u.seasonEnded <- r.Context().Err()
	case <-time.After(httpTimeout + 5*time.Second):
		t.Error("the season request was never cut off by any deadline")
	}
}

func TestStatsSeasonLegIsBounded(t *testing.T) {
	up := newSlowSeasonUpstream()
	p := newTestProvider(t, up.handler(t), providertest.Forbid(t))

	start := time.Now()
	reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ghosty", TimeWindow: "season"})
	elapsed := time.Since(start)

	assert.Equal(t, "player not found", reply.Error)
	assert.GreaterOrEqual(t, elapsed, seasonResolveTimeout,
		"the season leg cannot end before its own bound")
	assert.Less(t, elapsed, seasonResolveTimeout+2*time.Second,
		"the season leg must be released by seasonResolveTimeout, not by httpTimeout")

	select {
	case err := <-up.seasonEnded:
		assert.Error(t, err, "the season request must be cut off by its own deadline")
	case <-time.After(2 * time.Second):
		t.Fatal("the season upstream never saw the client hang up")
	}
}

func TestRequestsThatCannotBeServedAreRejectedBeforeAnyUpstreamCall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		endpoint  string
		req       gossiprpc.Request
		wantError string
	}{
		{"stats without an account", "stats", gossiprpc.Request{}, "missing account"},
		{"stats for a psn account", "stats", gossiprpc.Request{Account: "SomePlayer", AccountType: "psn"}, epicOnlyError},
		{"stats for an xbox account", "stats", gossiprpc.Request{Account: "SomePlayer", AccountType: "xbl"}, epicOnlyError},
		{"stats for an account type with stray spacing", "stats", gossiprpc.Request{Account: "SomePlayer", AccountType: "XBL "}, epicOnlyError},
		{"stats for a steam account", "stats", gossiprpc.Request{Account: "SomePlayer", AccountType: "steam"}, epicOnlyError},
		{"session_start without a channel", "session_start", gossiprpc.Request{Account: "Ninja"}, "missing account or channel"},
		{"session_start for a psn account", "session_start", gossiprpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "psn"}, epicOnlyError},
		{"session without an account", "session", gossiprpc.Request{ChannelID: "1"}, "missing account or channel"},
		{"session for an xbox account", "session", gossiprpc.Request{Account: "Ninja", ChannelID: "42", AccountType: "xbl"}, epicOnlyError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestProvider(t, providertest.Forbid(t), providertest.Forbid(t))

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), tc.req)

			assert.Equal(t, tc.wantError, providertest.ErrorOf(t, res))
		})
	}
}

func TestStatsAcceptEpicAccountsWithStraySpacing(t *testing.T) {
	p := newTestProvider(t, newStatsUpstream(t, "Ninja", syntheticBlob), providertest.Forbid(t))

	reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja", AccountType: " Epic "})

	assert.Empty(t, reply.Error)
}

func TestStatsUnknownPlayerNegativeCached(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{
		Status: http.StatusNotFound,
		Body:   `{"status":404,"error":"Upstream API error: Response status code does not indicate success: 404 (Not Found)."}`,
	})
	p := newTestProvider(t, upstream, providertest.Forbid(t))

	for range 2 {
		reply := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ghosty"})
		assert.Equal(t, "player not found", reply.Error)
	}
	assert.Equal(t, 1, upstream.Hits(), "unknown player must be served from the negative cache")
}

func TestSessionReportsGainsSinceTheStartSnapshot(t *testing.T) {
	up := newStatsUpstream(t, "Ninja", syntheticBlob)
	p := newTestProvider(t, up, providertest.Forbid(t))
	req := gossiprpc.Request{Account: "Ninja", ChannelID: "42"}

	snap := providertest.Call[gossiprpc.FortniteSnapshotReply](t, p, "session_start", req)
	require.Empty(t, snap.Error)
	assert.Equal(t, "Ninja", snap.Player)

	up.setBody(`{"accountId":"deadbeef","stats":{
		"br_placetop1_keyboardmouse_m0_playlist_defaultsolo": 25,
		"br_matchesplayed_keyboardmouse_m0_playlist_defaultsolo": 170,
		"br_kills_keyboardmouse_m0_playlist_defaultsolo": 600
	}}`)

	sess := providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", req)

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

func TestSessionBaselineBelongsToOneAccount(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startAccount string
		wantSnapshot [2]bool
	}{
		{"the first session read starts tracking", "", [2]bool{false, true}},
		{"a baseline stored for another account is not diffed against", "SomeoneElse", [2]bool{false, true}},
		{"a baseline stored for the same account is kept", "Ninja", [2]bool{true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newStatsUpstream(t, "Ninja", syntheticBlob).alsoKnows("SomeoneElse")
			p := newTestProvider(t, up, providertest.Forbid(t))
			if tc.startAccount != "" {
				start := providertest.Call[gossiprpc.FortniteSnapshotReply](t, p, "session_start", gossiprpc.Request{Account: tc.startAccount, ChannelID: "77"})
				require.Empty(t, start.Error)
			}

			for i, want := range tc.wantSnapshot {
				sess := providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", gossiprpc.Request{Account: "Ninja", ChannelID: "77"})
				require.Empty(t, sess.Error)
				assert.Equal(t, want, sess.HasSnapshot, "read %d", i+1)
				assert.Equal(t, "Ninja", sess.Player)
				assert.Zero(t, sess.Wins)
				assert.Zero(t, sess.Matches)
				assert.Zero(t, sess.Kills)
			}
		})
	}
}

func TestSessionEndDropsTheBaseline(t *testing.T) {
	p := newTestProvider(t, newStatsUpstream(t, "Ninja", syntheticBlob), providertest.Forbid(t))
	req := gossiprpc.Request{Account: "Ninja", ChannelID: "42"}
	require.Empty(t, providertest.Call[gossiprpc.FortniteSnapshotReply](t, p, "session_start", req).Error)

	ended := providertest.Call[gossiprpc.FortniteSnapshotReply](t, p, "session_end", req)
	sess := providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", req)

	assert.Empty(t, ended.Error)
	assert.False(t, sess.HasSnapshot, "an ended session must start tracking afresh")
	assert.Equal(t, "missing channel", providertest.Call[gossiprpc.FortniteSnapshotReply](t, p, "session_end", gossiprpc.Request{}).Error)
}

func TestStatsAndSessionShareOneUpstreamFetch(t *testing.T) {
	up := newStatsUpstream(t, "Ninja", syntheticBlob)
	p := newTestProvider(t, up, providertest.Forbid(t))

	stats := providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja"})
	require.Empty(t, stats.Error)
	warm := up.paths()
	require.NotEmpty(t, warm, "the cold stats lookup must reach the upstream")

	sess := providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", gossiprpc.Request{Account: "Ninja", ChannelID: "42"})
	require.Empty(t, sess.Error)
	assert.Equal(t, stats.Player, sess.Player)

	assert.Equal(t, warm, up.paths(), "a warm !fnstats must make the session path cost no upstream call at all")
}

func TestStatsEntryExpiryDoesNotRedoTheAccountResolve(t *testing.T) {
	up := newStatsUpstream(t, "Ninja", syntheticBlob)
	p, store := newProviderWithStore(t, up, providertest.Forbid(t))
	ctx := context.Background()

	require.Empty(t, providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja"}).Error)
	require.Equal(t, []string{displayNamePath + "Ninja", statsLookupPath}, up.paths(),
		"a cold lookup pays both upstream calls, in series")

	require.NoError(t, store.Del(ctx, "gossip:fortnite:stats:lifetime:ninja"))

	require.Empty(t, providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", gossiprpc.Request{Account: "Ninja"}).Error)
	assert.Equal(t, []string{displayNamePath + "Ninja", statsLookupPath, statsLookupPath}, up.paths(),
		"the refill must cost the stats call alone")
}

func TestSessionSurfacesSharedNegativeEntry(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{
		Status: http.StatusNotFound,
		Body:   `{"error":"Upstream API error: account not found"}`,
	})
	p := newTestProvider(t, upstream, providertest.Forbid(t))
	req := gossiprpc.Request{Account: "Ghosty", ChannelID: "42"}

	sess := providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", req)
	assert.Equal(t, "player not found", sess.Error)
	assert.False(t, sess.HasSnapshot)
	assert.Zero(t, sess.Matches, "a shaped failure must never read as a played session")

	before := upstream.Hits()
	sess = providertest.Call[gossiprpc.FortniteSessionReply](t, p, "session", req)
	assert.Equal(t, "player not found", sess.Error)
	assert.Equal(t, before, upstream.Hits(), "the negative entry must serve the repeat lookup")
}

func TestCacheKeysFoldWindowAndAccount(t *testing.T) {
	up := newStatsUpstream(t, "Ninja", syntheticBlob)
	p, store := newProviderWithStore(t, up, providertest.Forbid(t))

	for _, req := range []gossiprpc.Request{
		{Account: "Ninja", TimeWindow: "season"},
		{Account: "  NiNjA  ", TimeWindow: " SeAsOn "},
		{Account: "ninja"},
		{Account: "ninja", TimeWindow: "career"},
	} {
		_ = providertest.Call[gossiprpc.FortniteStatsReply](t, p, "stats", req)
	}

	assert.Equal(t, []string{
		"gossip:fortnite:account:ninja",
		"gossip:fortnite:season:start",
		"gossip:fortnite:stats:lifetime:ninja",
		"gossip:fortnite:stats:season:ninja",
	}, store.Keys())
}

func TestProviderDeclaresTheCommandsItShips(t *testing.T) {
	for _, tc := range []struct {
		name   string
		apiKey string
		want   []string
	}{
		{"a keyless deployment serves the item shop only", "", []string{"shop"}},
		{"a keyed deployment serves stats and sessions too", "k", []string{"shop", "stats", "session_start", "session", "session_end"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New(Config{APIKey: tc.apiKey}, providertest.Deps(providertest.NewMemStore()))

			var names []string
			for _, ep := range p.Endpoints() {
				names = append(names, ep.Name)
			}
			assert.ElementsMatch(t, tc.want, names)
		})
	}
}
