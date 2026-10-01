// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package paceman_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/paceman"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProviderWithStore(t testing.TB, handler http.Handler, store *providertest.MemStore) provider.Provider {
	return paceman.New(paceman.Config{BaseURL: providertest.Upstream(t, handler)}, providertest.Deps(store))
}

func newProvider(t testing.TB, handler http.Handler) provider.Provider {
	return newProviderWithStore(t, handler, providertest.NewMemStore())
}

func newUserProvider(t testing.TB, handler http.Handler) provider.Provider {
	url := providertest.Upstream(t, handler)
	return paceman.New(paceman.Config{BaseURL: url, UserBaseURL: url}, providertest.Deps(providertest.NewMemStore()))
}

const (
	sessionStatsBody = `{
		"nether": {"count": 3, "avg": "1:42"},
		"bastion": {"count": 2, "avg": "3:55"},
		"fortress": {"count": 2, "avg": "7:12"},
		"first_portal": {"count": 2, "avg": "9:20"},
		"stronghold": {"count": 1, "avg": "12:05"},
		"end": {"count": 1, "avg": "13:50"},
		"finish": {"count": 0, "avg": "0:00"},
		"truncated": false
	}`
	emptySessionStatsBody = `{
		"nether": {"count": 0, "avg": "0:00"},
		"bastion": {"count": 0, "avg": "0:00"},
		"fortress": {"count": 0, "avg": "0:00"},
		"first_portal": {"count": 0, "avg": "0:00"},
		"stronghold": {"count": 0, "avg": "0:00"},
		"end": {"count": 0, "avg": "0:00"},
		"finish": {"count": 0, "avg": "0:00"},
		"truncated": false
	}`
	sessionNethersBody          = `{"count": 3, "avg": "1:42", "rnph": 21.4, "uuid": "9a8e24df"}`
	untrackedSessionNethersBody = `{"count": 3, "avg": "1:42", "rnph": 0, "uuid": "9a8e24df"}`

	pbBody = `{
		"user": {"uuid": "9a8e", "twitchId": "1", "daily": 0, "weekly": 0, "monthly": 0, "bonus": 1, "score": 1},
		"completions": [],
		"pbs": {
			"daily": {"_id": "1", "submitted": 1716945000000, "time": 400123},
			"weekly": {"_id": "2", "submitted": 1716945000000, "time": 390456},
			"monthly": {"_id": "3", "submitted": 1716945000000, "time": 380789},
			"allTime": {"_id": "4", "submitted": 1716945000000, "time": 370012}
		}
	}`
	pbEmptyBody = `{
		"user": {"uuid": "9a8e", "twitchId": "1", "daily": 0, "weekly": 0, "monthly": 0, "bonus": 1, "score": 1},
		"completions": [],
		"pbs": {"daily": null, "weekly": null, "monthly": null, "allTime": null}
	}`
	notFoundBody = `{"error":"unknown player"}`
)

func sessionUpstream(t testing.TB, stats, nethers string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getSessionStats/":
			assert.Equal(t, "Feinberg", r.URL.Query().Get("name"))
			assert.Equal(t, "6", r.URL.Query().Get("hoursBetween"))
			_, _ = w.Write([]byte(stats))
		case "/getSessionNethers/":
			_, _ = w.Write([]byte(nethers))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
}

func TestSessionReplies(t *testing.T) {
	providertest.RunCases(t, newProvider, "session",
		[]providertest.Case[gossiprpc.PacemanSessionReply]{
			{Name: "averages every split of a tracked session", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: sessionUpstream(t, sessionStatsBody, sessionNethersBody),
				Want: gossiprpc.PacemanSessionReply{
					Player: "Feinberg", NetherCount: 3, Nether: "1:42", Bastion: "3:55", Fortress: "7:12", FirstPortal: "9:20",
					Stronghold: "12:05", End: "13:50", Finish: "0:00", NPH: 21.4,
				}},
			{Name: "reads a zero nether count as an empty session, not an error", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: sessionUpstream(t, emptySessionStatsBody, untrackedSessionNethersBody),
				Want: gossiprpc.PacemanSessionReply{
					Player: "Feinberg", Nether: "0:00", Bastion: "0:00", Fortress: "0:00", FirstPortal: "0:00",
					Stronghold: "0:00", End: "0:00", Finish: "0:00", Empty: true,
				}},
			{Name: "answers an upstream 4xx as an unknown player", Req: gossiprpc.Request{Account: "ghost"},
				Upstream: providertest.Respond(http.StatusNotFound, notFoundBody),
				Want:     gossiprpc.PacemanSessionReply{Player: "ghost", Error: "player not found"}},
			{Name: "rejects a missing account before any upstream call", Req: gossiprpc.Request{},
				Upstream: providertest.Forbid(t),
				Want:     gossiprpc.PacemanSessionReply{Error: "missing account"}},
			{Name: "asks chat to wait when PaceMan throttles", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: providertest.Respond(http.StatusTooManyRequests, `{}`),
				Want:     gossiprpc.PacemanSessionReply{Player: "Feinberg", Error: "PaceMan is busy, try again in a minute"}},
			{Name: "falls back to a generic failure on an upstream outage", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: providertest.Respond(http.StatusBadGateway, `{}`),
				Want:     gossiprpc.PacemanSessionReply{Player: "Feinberg", Error: "stats lookup failed"}},
		})
}

func TestNethersReplies(t *testing.T) {
	providertest.RunCases(t, newProvider, "nethers",
		[]providertest.Case[gossiprpc.PacemanNethersReply]{
			{Name: "reports the nether count, average, and rate", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: providertest.Respond(http.StatusOK, sessionNethersBody),
				Want:     gossiprpc.PacemanNethersReply{Player: "Feinberg", Count: 3, Avg: "1:42", NPH: 21.4}},
			{Name: "reads zero nethers as empty", Req: gossiprpc.Request{Account: "Newbie"},
				Upstream: providertest.Respond(http.StatusOK, `{"count": 0, "avg": "0:00", "rnph": 0}`),
				Want:     gossiprpc.PacemanNethersReply{Player: "Newbie", Empty: true, Avg: "0:00"}},
			{Name: "answers an upstream 4xx as an unknown player", Req: gossiprpc.Request{Account: "ghost"},
				Upstream: providertest.Respond(http.StatusBadRequest, `{"error":"bad request"}`),
				Want:     gossiprpc.PacemanNethersReply{Player: "ghost", Error: "player not found"}},
			{Name: "rejects a missing account before any upstream call", Req: gossiprpc.Request{},
				Upstream: providertest.Forbid(t),
				Want:     gossiprpc.PacemanNethersReply{Error: "missing account"}},
		})
}

func TestLastFortReplies(t *testing.T) {
	providertest.RunCases(t, newProvider, "lastfort",
		[]providertest.Case[gossiprpc.PacemanLastFortReply]{
			{Name: "reads an empty timestamp array as no recent fortress pace, not an error", Req: gossiprpc.Request{Account: "Feinberg"},
				Upstream: providertest.Respond(http.StatusOK, `[]`),
				Want:     gossiprpc.PacemanLastFortReply{Player: "Feinberg", Empty: true}},
			{Name: "answers an upstream 4xx as an unknown player", Req: gossiprpc.Request{Account: "ghost"},
				Upstream: providertest.Respond(http.StatusBadRequest, notFoundBody),
				Want:     gossiprpc.PacemanLastFortReply{Player: "ghost", Error: "player not found"}},
			{Name: "rejects a missing account before any upstream call", Req: gossiprpc.Request{},
				Upstream: providertest.Forbid(t),
				Want:     gossiprpc.PacemanLastFortReply{Error: "missing account"}},
		})
}

func jsonFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestLastFortRendersSplitsRelativeToTheRunStart(t *testing.T) {
	start := float64(time.Now().Add(-10 * time.Minute).Unix())
	body := `[{
		"start": ` + jsonFloat(start) + `,
		"nether": ` + jsonFloat(start+90) + `,
		"bastion": ` + jsonFloat(start+165) + `,
		"fortress": ` + jsonFloat(start+300) + `,
		"first_portal": null,
		"stronghold": null
	}]`
	p := newProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/getRecentTimestamps/", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("onlyFort"))
		_, _ = w.Write([]byte(body))
	}))

	reply := providertest.Call[gossiprpc.PacemanLastFortReply](t, p, "lastfort", gossiprpc.Request{Account: "Feinberg"})

	assert.InDelta(t, 600, reply.AgoSeconds, 5)
	reply.AgoSeconds = 0
	assert.Equal(t, gossiprpc.PacemanLastFortReply{Player: "Feinberg", Nether: "1:30", Bastion: "2:45", Fortress: "5:00"}, reply,
		"a null split must render blank, not a bogus duration")
}

func TestPersonalBestReplies(t *testing.T) {
	pb := func(t testing.TB) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/user", r.URL.Path)
			assert.Equal(t, "Feinberg", r.URL.Query().Get("name"))
			assert.Equal(t, "1", r.URL.Query().Get("sortByTime"))
			_, _ = w.Write([]byte(pbBody))
		}
	}
	window := func(name, timeWindow, reported, want string) providertest.Case[gossiprpc.PacemanPersonalBestReply] {
		return providertest.Case[gossiprpc.PacemanPersonalBestReply]{
			Name: name, Req: gossiprpc.Request{Account: "Feinberg", TimeWindow: timeWindow}, Upstream: pb(t),
			Want: gossiprpc.PacemanPersonalBestReply{Player: "Feinberg", Window: reported, Time: want},
		}
	}

	providertest.RunCases(t, newUserProvider, "personal_best",
		[]providertest.Case[gossiprpc.PacemanPersonalBestReply]{
			window("picks the daily best", "daily", "daily", "6:40.123"),
			window("picks the weekly best", "weekly", "weekly", "6:30.456"),
			window("picks the monthly best", "monthly", "monthly", "6:20.789"),
			window("defaults to the all-time best", "", "all-time", "6:10.012"),
			window("accepts the all-time spelling", "all-time", "all-time", "6:10.012"),
			{Name: "reads a null pb for the window as no personal best, not an error",
				Req:      gossiprpc.Request{Account: "Newbie", TimeWindow: "daily"},
				Upstream: providertest.Respond(http.StatusOK, pbEmptyBody),
				Want:     gossiprpc.PacemanPersonalBestReply{Player: "Newbie", Window: "daily", Empty: true}},
			{Name: "answers an upstream 4xx as an unknown player", Req: gossiprpc.Request{Account: "ghost"},
				Upstream: providertest.Respond(http.StatusNotFound, `Failed to find user with uuid: UNKNOWN`),
				Want:     gossiprpc.PacemanPersonalBestReply{Player: "ghost", Window: "all-time", Error: "player not found"}},
			{Name: "rejects a missing account before any upstream call", Req: gossiprpc.Request{},
				Upstream: providertest.Forbid(t),
				Want:     gossiprpc.PacemanPersonalBestReply{Window: "all-time", Error: "missing account"}},
		})
}

func TestCacheKeysFoldAccountAndWindow(t *testing.T) {
	store := providertest.NewMemStore()
	p := newProviderWithStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getSessionStats/":
			_, _ = w.Write([]byte(sessionStatsBody))
		case "/getSessionNethers/":
			_, _ = w.Write([]byte(sessionNethersBody))
		case "/getRecentTimestamps/":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(pbBody))
		}
	}), store)

	for _, endpoint := range []string{"session", "lastfort", "personal_best"} {
		_ = providertest.Endpoint(t, p, endpoint)(t.Context(), gossiprpc.Request{Account: "  FrOsTy  "})
	}

	assert.Equal(t, []string{
		"gossip:paceman:lastfort:frosty:0",
		"gossip:paceman:personal-best:frosty:0",
		"gossip:paceman:session-nethers:frosty:6",
		"gossip:paceman:session-stats:frosty:6",
	}, store.Keys())
}
