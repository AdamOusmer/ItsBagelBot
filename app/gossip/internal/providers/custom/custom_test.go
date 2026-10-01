// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func init() { core.SetSSRFCheckForTests(false) }

type fakeDefs struct {
	defs map[string]gossiprpc.FetchDef
	err  error
}

func (f fakeDefs) FetchDef(_ context.Context, _, name string) (gossiprpc.FetchDef, bool, error) {
	if f.err != nil {
		return gossiprpc.FetchDef{}, false, f.err
	}
	d, ok := f.defs[name]
	return d, ok, nil
}

type staged struct {
	status int
	ct     string
	body   string
	delay  time.Duration
}

type harness struct {
	p     *api
	store *providertest.MemStore
	socks *providertest.FakeSOCKS

	srv   *httptest.Server
	hits  atomic.Int32
	admit atomic.Int32

	routesMu sync.Mutex
	routes   map[string]staged

	defsMu sync.Mutex
	defs   map[string]gossiprpc.FetchDef
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		store:  providertest.NewMemStore(),
		socks:  providertest.NewFakeSOCKS(t),
		routes: map[string]staged{},
		defs:   map[string]gossiprpc.FetchDef{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.hits.Add(1)
		h.routesMu.Lock()
		st, ok := h.routes[r.URL.Path]
		h.routesMu.Unlock()
		if !ok {
			http.Error(w, "no route staged for "+r.URL.Path, http.StatusNotFound)
			return
		}
		if st.delay > 0 {
			time.Sleep(st.delay)
		}
		w.Header().Set("Content-Type", st.ct)
		w.WriteHeader(st.status)
		_, _ = w.Write([]byte(st.body))
	})
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)

	deps := provider.Deps{
		Cache:     core.NewCache(h.store),
		Log:       zap.NewNop(),
		FetchDefs: fakeDefs{defs: h.defs},
	}
	b := provider.NewProvider(providerName, deps)
	cfg := Config{ChannelRateLimit: 6, DefRateLimit: 30, HostRateLimit: 120, PositiveTTL: time.Minute}
	h.p = newAPI(cfg, deps, b)
	h.p.admit = func(context.Context, *flight, bool) error {
		h.admit.Add(1)
		return nil
	}
	return h
}

func (h *harness) route(t *testing.T, path string, r staged) {
	t.Helper()
	h.routesMu.Lock()
	h.routes[path] = r
	h.routesMu.Unlock()
}

func (h *harness) addDef(name, path string, def gossiprpc.FetchDef) gossiprpc.FetchDef {
	def.Name = name
	def.URL = h.srv.URL + path
	h.defs[name] = def
	return def
}

func call(t *testing.T, h *harness, req gossiprpc.Request) gossiprpc.CustomFetchReply {
	t.Helper()
	res := h.p.fetch(context.Background(), req)
	reply, ok := res.(gossiprpc.CustomFetchReply)
	require.True(t, ok, "handler returned %T", res)
	return reply
}

type fetchOutcome struct {
	Status gossiprpc.FetchStatus
	Values []string
}

func outcomeOf(reply gossiprpc.CustomFetchReply) fetchOutcome {
	return fetchOutcome{Status: reply.Status, Values: reply.Values}
}

func TestFetchShapesTheUpstreamResponseForChat(t *testing.T) {
	jsonRoute := func(body string) staged {
		return staged{status: http.StatusOK, ct: "application/json", body: body}
	}
	for _, tc := range []struct {
		name  string
		route staged
		def   gossiprpc.FetchDef
		defID string
		want  fetchOutcome
	}{
		{"extracts a nested json path", jsonRoute(`{"data":{"items":[{"name":"Shiny Thing"},{"name":"Other"}]},"n":42,"ok":true}`),
			gossiprpc.FetchDef{JSONPath: []string{"data", "items", "0", "name"}}, "d",
			fetchOutcome{gossiprpc.FetchOK, []string{"Shiny Thing"}}},
		{"a token tail overrides the stored path and keeps numbers raw", jsonRoute(`{"a":"first","b":42,"c":true}`),
			gossiprpc.FetchDef{JSONPath: []string{"a"}}, "d.b",
			fetchOutcome{gossiprpc.FetchOK, []string{"42"}}},
		{"a token tail can address a boolean", jsonRoute(`{"a":"first","b":42,"c":true}`),
			gossiprpc.FetchDef{JSONPath: []string{"a"}}, "d.c",
			fetchOutcome{gossiprpc.FetchOK, []string{"true"}}},
		{"a plain definition returns the trimmed body text", staged{status: http.StatusOK, ct: "text/plain", body: "  hello from upstream\n"},
			gossiprpc.FetchDef{}, "d",
			fetchOutcome{gossiprpc.FetchOK, []string{"hello from upstream"}}},
		{"caps a long value to the chat limit", staged{status: http.StatusOK, ct: "text/plain", body: strings.Repeat("x", 500)},
			gossiprpc.FetchDef{}, "d",
			fetchOutcome{gossiprpc.FetchOK, []string{strings.Repeat("x", maxValueRunes)}}},
		{"refuses a content type chat must not read", staged{status: http.StatusOK, ct: "application/octet-stream", body: "\xde\xad"},
			gossiprpc.FetchDef{}, "d",
			fetchOutcome{Status: gossiprpc.FetchUpstreamError}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.route(t, "/r", tc.route)
			tc.def.IsActive = true
			h.addDef("d", "/r", tc.def)

			reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: tc.defID})

			assert.Equal(t, tc.want, outcomeOf(reply))
			assert.GreaterOrEqual(t, reply.MS, 0)
		})
	}
}

func TestFetchCachesOnlyAnswersThatTeachSomething(t *testing.T) {
	for _, tc := range []struct {
		name          string
		route         staged
		def           gossiprpc.FetchDef
		wantStatus    gossiprpc.FetchStatus
		wantRetention time.Duration
		wantHits      int32
	}{
		{"a path that does not resolve is a bad definition cached briefly",
			staged{status: http.StatusOK, ct: "application/json", body: `{"a":"x"}`}, gossiprpc.FetchDef{JSONPath: []string{"nope"}},
			gossiprpc.FetchBadDef, 2 * negativeTTL, 1},
		{"an upstream 404 is cached briefly",
			staged{status: http.StatusNotFound, ct: "application/json", body: `{"error":"nope"}`}, gossiprpc.FetchDef{},
			gossiprpc.FetchUpstreamError, 2 * negativeTTL, 1},
		{"an upstream outage is never cached",
			staged{status: http.StatusInternalServerError, ct: "text/plain", body: "dead"}, gossiprpc.FetchDef{},
			gossiprpc.FetchUpstreamError, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.route(t, "/r", tc.route)
			tc.def.IsActive = true
			def := h.addDef("d", "/r", tc.def)

			for range 2 {
				assert.Equal(t, tc.wantStatus, call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "d"}).Status)
			}

			assert.Equal(t, tc.wantRetention, h.store.Retention(storedResultKey("ch1", def)))
			assert.Equal(t, tc.wantHits, h.hits.Load())
		})
	}
}

func TestFetchPositiveCachesThenFreshBypassesReadButWrites(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"v"}})

	first := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, first.Status)
	assert.Equal(t, int32(1), h.hits.Load())

	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":2}`})

	cached := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, cached.Status)
	assert.Equal(t, []string{"1"}, cached.Values, "normal read serves the stored entry")
	assert.Equal(t, int32(1), h.hits.Load())

	fresh := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", Fresh: true})
	require.Equal(t, gossiprpc.FetchOK, fresh.Status)
	assert.Equal(t, []string{"2"}, fresh.Values, "fresh must skip the positive-cache read")
	assert.Equal(t, int32(2), h.hits.Load())

	again := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	assert.Equal(t, []string{"2"}, again.Values, "the fresh result must have been written back")
	assert.Equal(t, int32(2), h.hits.Load())
}

func TestFetchDryRunSpendsNoBucketWritesNoCache(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"v"}})

	r1 := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})
	r2 := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})

	for _, r := range []gossiprpc.CustomFetchReply{r1, r2} {
		require.Equal(t, gossiprpc.FetchOK, r.Status)
		assert.Equal(t, []string{"1"}, r.Values)
	}
	assert.Equal(t, int32(2), h.hits.Load(), "dry runs execute for real")
	assert.Equal(t, int32(0), h.admit.Load(), "dry runs spend no bucket")
	assert.Empty(t, h.store.Keys(), "dry runs write no cache")
}

func TestFetchSamplesAreOnlyForTheAuthoringTool(t *testing.T) {
	const body = `{"forecast":{"temp":71.2}}`
	oversized := strings.Repeat("a", maxSampleBytes+1)
	for _, tc := range []struct {
		name       string
		ct         string
		body       string
		path       []string
		dryRun     bool
		wantStatus gossiprpc.FetchStatus
		wantSample string
	}{
		{"a dry run returns the real response for the field picker", "application/json", body, []string{"forecast", "temp"}, true, gossiprpc.FetchOK, body},
		{"chat never receives upstream text", "application/json", body, []string{"forecast", "temp"}, false, gossiprpc.FetchOK, ""},
		{"an author whose path is wrong still gets the tree", "application/json", body, []string{"nope"}, true, gossiprpc.FetchBadDef, body},
		{"an oversized body is dropped rather than truncated, since a half body could parse as a shorter document with different paths",
			"text/plain", oversized, nil, true, gossiprpc.FetchOK, ""},
		{"a body that is not utf-8 would not survive JSON marshalling", "text/plain", "\xff\xfe\x00", nil, true, gossiprpc.FetchOK, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.route(t, "/wx", staged{status: http.StatusOK, ct: tc.ct, body: tc.body})
			h.addDef("wx", "/wx", gossiprpc.FetchDef{IsActive: true, JSONPath: tc.path})

			reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: tc.dryRun})

			assert.Equal(t, tc.wantStatus, reply.Status)
			assert.Equal(t, tc.wantSample, reply.Sample)
		})
	}
}

func TestFetchBucketDenialAnswersLimited(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})
	h.p.admit = func(context.Context, *flight, bool) error {
		return &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}
	}

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})

	assert.Equal(t, gossiprpc.FetchLimited, reply.Status)
	assert.Zero(t, h.hits.Load(), "a bucket denial never reaches the upstream")
	assert.Empty(t, h.store.Keys(), "denials are retried on the next request, never pinned")
}

func TestFetchPremiumRidesAdmitLane(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	var gotPremium atomic.Bool
	var gotHost string
	h.p.admit = func(_ context.Context, fl *flight, isPremium bool) error {
		gotPremium.Store(isPremium)
		gotHost = fl.host
		return nil
	}
	call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", IsPremium: true})

	assert.True(t, gotPremium.Load())
	assert.NotEmpty(t, gotHost, "the per-host layer needs the target host")
}

func TestBreakerArmsAfterFiveConsecutiveTransportFailures(t *testing.T) {
	h := newHarness(t)
	h.defs["dead"] = gossiprpc.FetchDef{
		Name:     "dead",
		URL:      "https://blackhole.invalid/never",
		IsActive: true,
	}

	var last gossiprpc.CustomFetchReply
	for i := 1; i <= breakerThreshold; i++ {
		last = call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "dead"})
		assert.NotEqual(t, gossiprpc.FetchOK, last.Status)
	}
	assert.Equal(t, gossiprpc.FetchTimeout, last.Status,
		"transport failure without an answer maps to timeout, the infra family sesame now renders empty (letting |fallback speak) rather than as authored English")

	h.defs["samehost"] = gossiprpc.FetchDef{
		Name:     "samehost",
		URL:      "https://blackhole.invalid/unreachable-but-armed",
		IsActive: true,
	}
	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "samehost"})
	assert.Equal(t, gossiprpc.FetchLimited, reply.Status, "five consecutive transport failures arm the fleet-wide circuit: the armed host answers limited without dialing")
}

func TestBreakerStaysOpenBelowTheThresholdAndAfterAReset(t *testing.T) {
	h := newHarness(t)
	h.defs["flap"] = gossiprpc.FetchDef{Name: "flap", URL: "https://" + hostOf(h.srv.URL) + "/unreachable-host-route", IsActive: true}
	h.socks.SetRefusing(true)
	for i := 0; i < breakerThreshold-1; i++ {
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "flap"})
		assert.Equal(t, gossiprpc.FetchTimeout, reply.Status, "%d failures alone must not arm: failure %d is still attempted", breakerThreshold-1, i+1)
	}

	h.socks.SetRefusing(false)
	h.route(t, "/alive", staged{status: http.StatusInternalServerError, ct: "text/plain", body: "answering, badly"})
	h.defs["flap"] = gossiprpc.FetchDef{Name: "flap", URL: h.srv.URL + "/alive", IsActive: true}
	for i := 0; i <= breakerThreshold-1; i++ {
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "flap"})
		assert.Equal(t, gossiprpc.FetchUpstreamError, reply.Status, "an answering host is never limited, so the breaker stayed unarmed")
	}
}

func hostOf(raw string) string {
	return strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
}

func TestFetchRefusesDefinitionsThatCannotRunSafely(t *testing.T) {
	for _, tc := range []struct {
		name  string
		def   *gossiprpc.FetchDef
		defID string
	}{
		{"a definition that does not exist", nil, "ghost"},
		{"a paused definition never dials", &gossiprpc.FetchDef{IsActive: false}, "d"},
		{"a dangling key label fails closed instead of sending unauthenticated", &gossiprpc.FetchDef{IsActive: true, KeyLabel: "gone"}, "d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.route(t, "/r", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
			if tc.def != nil {
				h.addDef("d", "/r", *tc.def)
			}

			reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: tc.defID})

			assert.Equal(t, gossiprpc.FetchBadDef, reply.Status)
			assert.Zero(t, h.hits.Load())
		})
	}
}

func TestFetchInlineDefRehearsal(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/draft", staged{status: http.StatusOK, ct: "application/json", body: `{"temp_f":"71.2"}`})

	draft := &gossiprpc.FetchDef{
		Name:     "unsaved",
		URL:      h.srv.URL + "/draft",
		JSONPath: []string{"temp_f"},
		IsActive: true,
	}
	reply := call(t, h, gossiprpc.Request{ChannelID: "sesame_sam", DefID: "unsaved.temp_f", Def: draft, DryRun: true, Fresh: true})

	require.Equal(t, gossiprpc.FetchOK, reply.Status)
	assert.Equal(t, []string{"71.2"}, reply.Values)
	assert.Empty(t, h.store.Keys(), "inline drafts never touch the shared cache")
}

func TestFetchSlowUpstreamMapsToTimeout(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/slow", staged{status: http.StatusOK, ct: "application/json", body: `{}`, delay: 3 * time.Second})
	h.addDef("slow", "/slow", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "slow"})

	assert.Equal(t, gossiprpc.FetchTimeout, reply.Status)
}

func TestUnkeyedDefFetchesWithoutKeyResolver(t *testing.T) {
	h := newHarness(t)
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth":"` + r.Header.Get(authHeaderName) + `"}`))
	}))
	t.Cleanup(echo.Close)
	h.defs["open"] = gossiprpc.FetchDef{
		Name: "open", URL: echo.URL + "/echo", IsActive: true, KeyLabel: "", JSONPath: []string{"auth"},
	}

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "open"})

	assert.Equal(t, fetchOutcome{gossiprpc.FetchOK, []string{""}}, outcomeOf(reply),
		"an unkeyed def must fetch with no resolver wired, and no Authorization header may ride it")
}
