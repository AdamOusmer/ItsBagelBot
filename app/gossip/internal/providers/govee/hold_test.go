// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package govee_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/govee"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stateRoute   = "/router/api/v1/device/state"
	controlRoute = "/router/api/v1/device/control"
	testHoldKey  = "govee:hold:2:AB:CD:EF"
	testRateKey  = "ratelimit:gossip:govee:2"

	rgbState = `{"code":200,"payload":{"capabilities":[
		{"type":"devices.capabilities.online","instance":"online","state":{"value":true}},
		{"type":"devices.capabilities.on_off","instance":"powerSwitch","state":{"value":1}},
		{"type":"devices.capabilities.color_setting","instance":"colorRgb","state":{"value":3394611}},
		{"type":"devices.capabilities.color_setting","instance":"colorTemperatureK","state":{"value":""}},
		{"type":"devices.capabilities.range","instance":"brightness","state":{"value":25}}
	]}}`
	kelvinState = `{"code":200,"payload":{"capabilities":[
		{"type":"devices.capabilities.online","instance":"online","state":{"value":true}},
		{"type":"devices.capabilities.on_off","instance":"powerSwitch","state":{"value":1}},
		{"type":"devices.capabilities.color_setting","instance":"colorRgb","state":{"value":3394611}},
		{"type":"devices.capabilities.color_setting","instance":"colorTemperatureK","state":{"value":4500}},
		{"type":"devices.capabilities.range","instance":"brightness","state":{"value":""}}
	]}}`
	offState = `{"code":200,"payload":{"capabilities":[
		{"type":"devices.capabilities.online","instance":"online","state":{"value":true}},
		{"type":"devices.capabilities.on_off","instance":"powerSwitch","state":{"value":0}}
	]}}`
	offlineState = `{"code":200,"payload":{"capabilities":[
		{"type":"devices.capabilities.online","instance":"online","state":{"value":false}}
	]}}`
)

type lightCapability struct {
	Type     string `json:"type"`
	Instance string `json:"instance"`
	Value    int    `json:"value"`
}

type lightPayload struct {
	SKU        string           `json:"sku"`
	Device     string           `json:"device"`
	Capability *lightCapability `json:"capability"`
}

type lightRequest struct {
	Path    string
	Payload lightPayload
}

type lightUpstream struct {
	t       testing.TB
	replies map[int]providertest.Reply
	mu      sync.Mutex
	history []lightRequest
}

func (u *lightUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	assert.Equal(u.t, http.MethodPost, r.Method)
	assert.Equal(u.t, "k", r.Header.Get("Govee-API-Key"))
	var req struct {
		RequestID string       `json:"requestId"`
		Payload   lightPayload `json:"payload"`
	}
	body, err := io.ReadAll(r.Body)
	assert.NoError(u.t, err)
	assert.NoError(u.t, codec.Unmarshal(body, &req))
	assert.NotEmpty(u.t, req.RequestID)
	response := u.record(lightRequest{Path: r.URL.Path, Payload: req.Payload})
	if response.Status != 0 {
		w.WriteHeader(response.Status)
	}
	_, _ = io.WriteString(w, response.Body)
}

func (u *lightUpstream) record(req lightRequest) providertest.Reply {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.history = append(u.history, req)
	if reply, ok := u.replies[len(u.history)]; ok {
		return reply
	}
	assert.Equal(u.t, controlRoute, req.Path)
	return providertest.Reply{Body: `{"code":200}`}
}

func (u *lightUpstream) requests() []lightRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.history)
}

func lightRequests(req gossiprpc.Request, steps ...lightCapability) []lightRequest {
	var calls []lightRequest
	for _, step := range steps {
		calls = append(calls, lightRequest{Path: controlRoute, Payload: lightPayload{SKU: req.SKU, Device: req.Device, Capability: &step}})
	}
	return calls
}

func flashRequests(req gossiprpc.Request, steps ...lightCapability) []lightRequest {
	state := lightRequest{Path: stateRoute, Payload: lightPayload{SKU: req.SKU, Device: req.Device}}
	return append([]lightRequest{state}, lightRequests(req, steps...)...)
}

func newHoldProvider(t testing.TB, upstream *lightUpstream, cfg govee.Config, limiter *ratelimit.Limiter) (provider.Provider, *providertest.MemStore) {
	t.Helper()
	store := providertest.NewMemStore()
	deps := providertest.Deps(store)
	deps.GoveeKeys, deps.Limiter = fakeKeys{key: "k"}, limiter
	cfg.BaseURL = providertest.Upstream(t, upstream)
	return govee.New(cfg, deps), store
}

func flashRequest() gossiprpc.Request {
	req := sceneRequest()
	req.HoldS, req.HoldID, req.KeepLook = 5, "flash", true
	return req
}

func TestRestoreValidatesRequestsBeforeLookingUpTheKey(t *testing.T) {
	deps := providertest.Deps(providertest.NewMemStore())
	deps.GoveeKeys = fakeKeys{}
	p := govee.New(govee.Config{}, deps)
	for _, tc := range []struct {
		name string
		req  gossiprpc.Request
		want gossiprpc.GoveeRestoreReply
	}{
		{"missing channel", gossiprpc.Request{Device: "AB", SKU: "H6159", HoldID: "flash"}, gossiprpc.GoveeRestoreReply{Error: "missing channel"}},
		{"missing device", gossiprpc.Request{ChannelID: "2", HoldID: "flash"}, gossiprpc.GoveeRestoreReply{Error: "missing device"}},
		{"missing hold id", sceneRequest(), gossiprpc.GoveeRestoreReply{Error: "missing hold id"}},
		{"missing lease", flashRequest(), gossiprpc.GoveeRestoreReply{OK: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", tc.req)
			assert.Equal(t, tc.want, reply)
		})
	}
}

func TestRestoreAppliesTheSnapshot(t *testing.T) {
	on := lightCapability{"devices.capabilities.on_off", "powerSwitch", 1}
	off := lightCapability{"devices.capabilities.on_off", "powerSwitch", 0}
	for _, tc := range []struct {
		name  string
		state providertest.Reply
		steps []lightCapability
		want  gossiprpc.GoveeRestoreReply
	}{
		{"RGB and brightness", providertest.Reply{Body: rgbState}, []lightCapability{on, {"devices.capabilities.color_setting", "colorRgb", 3394611}, {"devices.capabilities.range", "brightness", 25}}, gossiprpc.GoveeRestoreReply{OK: true, Restored: 1}},
		{"Kelvin takes precedence over RGB", providertest.Reply{Body: kelvinState}, []lightCapability{on, {"devices.capabilities.color_setting", "colorTemperatureK", 4500}}, gossiprpc.GoveeRestoreReply{OK: true, Restored: 1}},
		{"light was off", providertest.Reply{Body: offState}, []lightCapability{off}, gossiprpc.GoveeRestoreReply{OK: true, Restored: 1}},
		{"light was offline", providertest.Reply{Body: offlineState}, nil, gossiprpc.GoveeRestoreReply{OK: true}},
		{"state read failed", providertest.Reply{Status: http.StatusInternalServerError, Body: `{}`}, nil, gossiprpc.GoveeRestoreReply{OK: true}},
		{"online status missing", providertest.Reply{Body: `{"code":200,"payload":{}}`}, nil, gossiprpc.GoveeRestoreReply{OK: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &lightUpstream{t: t, replies: map[int]providertest.Reply{1: tc.state}}
			p, store := newHoldProvider(t, upstream, govee.Config{}, nil)
			flash := flashRequest()
			changed := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", flash)
			require.Equal(t, gossiprpc.GoveeControlReply{OK: true}, changed)
			assert.Equal(t, 65*time.Second, store.Retention(testHoldKey))

			r := groupRequest("AB:CD:EF", "11:22:33")
			r.HoldID = flash.HoldID
			reply := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", r)

			assert.Equal(t, tc.want, reply)
			assert.Equal(t, append(flashRequests(flash, on), lightRequests(flash, tc.steps...)...), upstream.requests())
			assert.Empty(t, store.Keys())
		})
	}
}

func TestLatestControlOwnsTheHold(t *testing.T) {
	on := lightCapability{"devices.capabilities.on_off", "powerSwitch", 1}
	off := lightCapability{"devices.capabilities.on_off", "powerSwitch", 0}
	flash := flashRequest()
	for _, tc := range []struct {
		name       string
		holdS      int
		steps      []lightRequest
		responses  map[int]providertest.Reply
		wantLatest gossiprpc.GoveeRestoreReply
	}{
		{"overlapping flash", 10, append(flashRequests(flash, on), lightRequests(flash, off)...),
			map[int]providertest.Reply{1: {Body: offState}, 3: {Body: rgbState}}, gossiprpc.GoveeRestoreReply{OK: true, Restored: 1}},
		{"plain power-on", 0, lightRequests(flash, on), map[int]providertest.Reply{1: {Body: offState}}, gossiprpc.GoveeRestoreReply{OK: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &lightUpstream{t: t, replies: tc.responses}
			p, store := newHoldProvider(t, upstream, govee.Config{}, nil)
			newer := flash
			newer.HoldS, newer.HoldID = tc.holdS, "newer"
			for _, req := range []gossiprpc.Request{flash, newer} {
				changed := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)
				require.Equal(t, gossiprpc.GoveeControlReply{OK: true}, changed)
			}

			stale := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", flash)
			latest := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", newer)

			assert.Equal(t, []gossiprpc.GoveeRestoreReply{{OK: true}, tc.wantLatest}, []gossiprpc.GoveeRestoreReply{stale, latest})
			assert.Equal(t, append(flashRequests(flash, on), tc.steps...), upstream.requests())
			assert.Empty(t, store.Keys())
		})
	}
}

func TestRestoreRetriesAfterARateLimit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rate       float64
		retryAfter time.Duration
		responses  map[int]providertest.Reply
		denied     []lightCapability
	}{
		{"local bucket", 1, 60 * time.Second, map[int]providertest.Reply{1: {Body: offState}}, nil},
		{"Govee HTTP 429", 0, 7500 * time.Millisecond,
			map[int]providertest.Reply{1: {Body: offState}, 3: {Status: http.StatusTooManyRequests, Body: `{}`}},
			[]lightCapability{{"devices.capabilities.on_off", "powerSwitch", 0}}},
		{"Govee body code 429", 0, 7500 * time.Millisecond,
			map[int]providertest.Reply{1: {Body: offState}, 3: {Body: `{"code":429}`}},
			[]lightCapability{{"devices.capabilities.on_off", "powerSwitch", 0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var limiter *ratelimit.Limiter
			refill := func() {}
			if tc.rate > 0 {
				client := valkeytest.Spawn(t)
				limiter = ratelimit.New(client)
				refill = func() { require.NoError(t, pkgvalkey.NewKV(client).Del(context.Background(), testRateKey)) }
			}
			upstream := &lightUpstream{t: t, replies: tc.responses}
			p, store := newHoldProvider(t, upstream, govee.Config{RateLimit: tc.rate}, limiter)
			exerciseRestoreRetry(t, p, tc.retryAfter, func() {
				assert.Equal(t, tc.retryAfter+time.Minute, store.Retention(testHoldKey))
				assert.Equal(t, []string{testHoldKey}, store.Keys())
				refill()
			})

			flash := flashRequest()
			want := flashRequests(flash, lightCapability{"devices.capabilities.on_off", "powerSwitch", 1})
			steps := append(slices.Clone(tc.denied), lightCapability{"devices.capabilities.on_off", "powerSwitch", 0})
			assert.Equal(t, append(want, lightRequests(flash, steps...)...), upstream.requests())
			assert.Empty(t, store.Keys())
		})
	}
}

func exerciseRestoreRetry(t testing.TB, p provider.Provider, retryAfter time.Duration, refill func()) {
	t.Helper()
	flash := flashRequest()
	stale := flash
	stale.HoldID = "expired"
	missing := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", stale)
	assert.Equal(t, gossiprpc.GoveeRestoreReply{OK: true}, missing)
	changed := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", flash)
	require.Equal(t, gossiprpc.GoveeControlReply{OK: true}, changed)
	wrongOwner := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", stale)
	assert.Equal(t, gossiprpc.GoveeRestoreReply{OK: true}, wrongOwner)
	denied := providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", flash)
	assert.Equal(t, gossiprpc.GoveeRestoreReply{RateLimited: true, RetryAfterMS: retryAfter.Milliseconds()}, denied)
	refill()
	assert.Equal(t, gossiprpc.GoveeRestoreReply{OK: true, Restored: 1}, providertest.Call[gossiprpc.GoveeRestoreReply](t, p, "restore", flash))
}
