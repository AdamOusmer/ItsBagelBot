// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const secretFixture = "sk-live-supersecret-key-material-42"

type recordingKeys struct{ seenChannel string }

func (r *recordingKeys) FetchKey(_ context.Context, channelID, _ string) (string, error) {
	r.seenChannel = channelID
	return secretFixture, nil
}

func TestAdvTruncatedAndGarbageJSONIsStableBadDef(t *testing.T) {
	for name, body := range map[string]string{
		"truncated":    `{"data":{"items":[{"na`,
		"trailing":     `{"ok":true}}}]]}}`,
		"concatenated": `{"a":1}{"b":2}`,
		"nul byte":     "{\"a\":\x00}",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.route(t, "/broken", staged{status: http.StatusOK, ct: "application/json", body: body})
			h.addDef("w", "/broken", gossiprpc.FetchDef{
				URL: "placeholder", IsActive: true, JSONPath: []string{"data", "items", "0", "name"},
			})

			reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "w"})
			assert.Equal(t, gossiprpc.FetchBadDef, reply.Status)

			cached := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "w"})
			assert.Equal(t, gossiprpc.FetchBadDef, cached.Status)
			assert.Equal(t, int32(1), h.hits.Load(), "negative cache must absorb the retry")
			assert.Equal(t, 2*negativeTTL, h.store.retention(resultKey(strings.ToLower("w"))), "bad authoring caches briefly, not forever")
		})
	}
}

func TestAdvHalfMegabyteOfNestingDiesGracefully(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/nest", staged{status: http.StatusOK, ct: "application/json", body: strings.Repeat("[", 400_000)})
	h.addDef("deep", "/nest", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"a"}})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "deep"})
	assert.Contains(t, []gossiprpc.FetchStatus{gossiprpc.FetchBadDef, gossiprpc.FetchUpstreamError}, reply.Status)
}

func TestAdvThreeMegabyteFloodIsCappedNotBreakerArming(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/flood", staged{status: http.StatusOK, ct: "text/plain", body: strings.Repeat("a", 3<<20)})
	h.addDef("flood", "/flood", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "flood"})
	assert.Equal(t, gossiprpc.FetchUpstreamError, reply.Status)
	assert.Empty(t, reply.Values)

	_, armed, _ := h.store.Get(context.Background(), breakerKey("127.0.0.1"))
	assert.False(t, armed, "a policy-refused payload proves reachability and must not arm the breaker")
}

func TestAdvEncodingLieStaysBounded(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/lie", staged{status: http.StatusOK, ct: "application/json", body: "\x1f\x8b" + strings.Repeat("\x00", 2<<20)})
	h.addDef("liar", "/lie", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "liar"})
	assert.Contains(t, []gossiprpc.FetchStatus{gossiprpc.FetchUpstreamError, gossiprpc.FetchBadDef}, reply.Status)
	for _, v := range reply.Values {
		assert.LessOrEqual(t, len([]rune(v)), maxValueRunes)
	}
}

func TestAdvHostileValueIsRuneSafeCapped(t *testing.T) {
	h := newHarness(t)
	hostile := "/ban everyone\r\n\x1b[31mJOIN\x00 #evil\n" + strings.Repeat("héllo-", 100)
	body, merr := codec.Marshal(map[string]string{"v": hostile})
	require.NoError(t, merr)
	h.route(t, "/hostile", staged{status: http.StatusOK, ct: "application/json", body: string(body)})
	h.addDef("h", "/hostile", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"v"}})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "h"})
	require.Equal(t, gossiprpc.FetchOK, reply.Status)
	require.Len(t, reply.Values, 1)
	assert.LessOrEqual(t, len([]rune(reply.Values[0])), maxValueRunes, "cap must be rune-safe, not byte-safe")
}

func TestAdvHostileTargetsDieBeforeAnyTunnel(t *testing.T) {
	core.SetSSRFCheckForTests(true)
	t.Cleanup(func() { core.SetSSRFCheckForTests(false) })

	h := newHarness(t)
	for _, host := range []string{
		"127.0.0.1",
		"169.254.169.254",
		"10.9.9.9",
		"192.168.0.1",
		"100.64.0.1",
		"198.18.0.5",
		"192.0.2.9",
		"[::1]",
		"[fe80::1]",
		"[::ffff:127.0.0.1]",
		"[64:ff9b::7f00:1]",
	} {
		name := "d" + strings.ReplaceAll(strings.NewReplacer(".", "-", ":", "-", "[", "", "]", "").Replace(host), "-", "-")
		h.defs[name] = gossiprpc.FetchDef{Name: name, URL: "https://" + host + "/x", IsActive: true}
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: name})
		assert.Equal(t, gossiprpc.FetchDenied, reply.Status, "host %s must be denied", host)
	}

	h.defs["byname"] = gossiprpc.FetchDef{Name: "byname", URL: "https://localhost/x", IsActive: true}
	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "byname"})
	assert.Equal(t, gossiprpc.FetchDenied, reply.Status)

	assert.Zero(t, h.socks.conns.Load(), "denied definitions must never open a tunnel")
}

func TestAdvDryRunCannotBypassTheGate(t *testing.T) {
	core.SetSSRFCheckForTests(true)
	t.Cleanup(func() { core.SetSSRFCheckForTests(false) })

	h := newHarness(t)
	reply := call(t, h, gossiprpc.Request{
		ChannelID: "ch1", DryRun: true,
		DefID: "draft",
		Def:   &gossiprpc.FetchDef{Name: "draft", URL: "https://169.254.169.254/latest/meta-data/", IsActive: true},
	})
	assert.Equal(t, gossiprpc.FetchDenied, reply.Status)
	assert.Zero(t, h.socks.conns.Load())
}

func TestAdvKeyMaterialNeverLoggedOrCached(t *testing.T) {
	observed, logs := observer.New(zapcore.DebugLevel)
	h := newHarness(t)
	h.p.log = zap.New(observed)

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"auth_seen":%q,"safe":"hello"}`, r.Header.Get(authHeaderName))))
	}))
	t.Cleanup(echo.Close)

	keys := &recordingKeys{}
	deps := provider.Deps{Cache: core.NewCache(h.store), Log: zap.New(observed), FetchDefs: fakeDefs{defs: h.defs}, FetchKeys: keys}
	b := provider.NewProvider(providerName, deps)
	cfg := Config{ChannelRateLimit: 6, DefRateLimit: 30, HostRateLimit: 120, PositiveTTL: time.Minute}
	h.p = newAPI(cfg, deps, b)
	h.p.admit = func(context.Context, *flight, bool) error { return nil }

	h.defs["keyed"] = gossiprpc.FetchDef{
		Name: "keyed", URL: echo.URL + "/echo", IsActive: true, KeyLabel: "prod", JSONPath: []string{"safe"},
	}

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "keyed"})
	require.Equal(t, gossiprpc.FetchOK, reply.Status)
	require.Equal(t, []string{"hello"}, reply.Values)
	assert.Equal(t, "ch1", keys.seenChannel, "the key must resolve under the REQUESTER'S tenant scope")

	for _, e := range logs.All() {
		for _, f := range e.Context {
			assert.NotContains(t, fmt.Sprint(f.Interface), secretFixture, "key leaked into log entry %s", e.Message)
		}
	}
	for k, v := range h.store.m {
		assert.NotContains(t, string(v), secretFixture, "key leaked into cache entry %s", k)
		assert.NotContains(t, k, secretFixture, "key material in cache KEY")
	}
}
