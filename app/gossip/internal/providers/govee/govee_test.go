// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package govee_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"

	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/govee"
	"ItsBagelBot/app/gossip/internal/providertest"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeKeys struct {
	key string
	err error
}

func (f fakeKeys) Key(context.Context, string) (string, error) { return f.key, f.err }

func newProvider(t testing.TB, keys provider.BroadcasterKeyResolver, handler http.Handler) provider.Provider {
	deps := providertest.Deps(providertest.NewMemStore())
	deps.GoveeKeys = keys
	return govee.New(govee.Config{BaseURL: providertest.Upstream(t, handler)}, deps)
}

const deviceListBody = `{
	"code": 200,
	"message": "success",
	"data": [
		{"sku":"H6159","device":"AB:CD:EF","deviceName":"Desk strip","capabilities":[
			{"type":"devices.capabilities.on_off","instance":"powerSwitch"},
			{"type":"devices.capabilities.color_setting","instance":"colorRgb"}
		]},
		{"sku":"H5081","device":"11:22:33","deviceName":"Smart plug","capabilities":[
			{"type":"devices.capabilities.on_off","instance":"powerSwitch"}
		]}
	]
}`

func TestDevicesParsesAndFlagsColor(t *testing.T) {
	var gotKey string
	p := newProvider(t, fakeKeys{key: "k-123"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/router/api/v1/user/devices", r.URL.Path)
		gotKey = r.Header.Get("Govee-API-Key")
		_, _ = io.WriteString(w, deviceListBody)
	}))

	reply := providertest.Call[gossiprpc.GoveeDevicesReply](t, p, "devices", gossiprpc.Request{ChannelID: "2"})

	assert.Equal(t, "k-123", gotKey, "the broadcaster's key must ride the header")
	assert.Equal(t, []gossiprpc.GoveeDevice{
		{Device: "AB:CD:EF", SKU: "H6159", Name: "Desk strip", Color: true},
		{Device: "11:22:33", SKU: "H5081", Name: "Smart plug"},
	}, reply.Devices)
}

func TestDevicesFreshBypassesCache(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{Body: deviceListBody})
	p := newProvider(t, fakeKeys{key: "k-123"}, upstream)

	providertest.Call[gossiprpc.GoveeDevicesReply](t, p, "devices", gossiprpc.Request{ChannelID: "2"})
	require.Equal(t, 1, upstream.Hits())

	providertest.Call[gossiprpc.GoveeDevicesReply](t, p, "devices", gossiprpc.Request{ChannelID: "2", Fresh: true})
	assert.Equal(t, 2, upstream.Hits())
}

func TestControlPowersOnThenSetsColor(t *testing.T) {
	var bodies []map[string]any
	var gotKey string
	var mu sync.Mutex
	p := newProvider(t, fakeKeys{key: "k-9"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/router/api/v1/device/control", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, codec.Unmarshal(b, &body))
		mu.Lock()
		gotKey = r.Header.Get("Govee-API-Key")
		bodies = append(bodies, body)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"code":200,"message":"success"}`)
	}))

	reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control",
		gossiprpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0x00CCFF})

	require.True(t, reply.OK)
	assert.Equal(t, "k-9", gotKey)
	require.Len(t, bodies, 2, "control is power-on then colour")
	assert.Equal(t, map[string]any{"type": "devices.capabilities.on_off", "instance": "powerSwitch", "value": float64(1)}, capabilityOf(t, bodies[0]))
	assert.Equal(t, map[string]any{"type": "devices.capabilities.color_setting", "instance": "colorRgb", "value": float64(0x00CCFF)}, capabilityOf(t, bodies[1]))
}

func TestRequestsThatCannotBeServedExplainWhyAndOnlyDialWhenTheyShould(t *testing.T) {
	const channel = "2"
	for _, tc := range []struct {
		name            string
		endpoint        string
		keys            fakeKeys
		req             gossiprpc.Request
		upstream        []providertest.Reply
		wantError       string
		wantHits        int
		wantKeyRejected bool
	}{
		{"devices: rejects a request without a channel", "devices", fakeKeys{key: "k"},
			gossiprpc.Request{}, nil, "missing channel", 0, false},
		{"devices: does not dial Govee with no key on file", "devices", fakeKeys{},
			gossiprpc.Request{ChannelID: channel}, nil, "no Govee API key", 0, false},
		{"devices: reports a key store failure", "devices", fakeKeys{err: errors.New("custody unreachable")},
			gossiprpc.Request{ChannelID: channel}, nil, "could not read your Govee key", 0, false},
		{"devices: reports a rejected key", "devices", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel}, []providertest.Reply{{Status: http.StatusUnauthorized, Body: `{}`}}, "Govee did not accept this API key", 1, true},
		{"devices: reports an upstream failure", "devices", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel}, []providertest.Reply{{Status: http.StatusInternalServerError, Body: `{}`}}, "device lookup failed", 1, false},
		{"control: does not dial Govee without a device", "control", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel, SKU: "H6159", ColorRGB: 1}, nil, "missing device", 0, false},
		{"control: surfaces an API level failure", "control", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel, Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0xFF0000},
			[]providertest.Reply{{Body: `{"code":400,"message":"invalid device"}`}}, "could not reach your lights", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.NewSequence(t, tc.upstream...)
			p := newProvider(t, tc.keys, upstream)

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), tc.req)

			assert.Contains(t, providertest.ErrorOf(t, res), tc.wantError)
			if tc.endpoint == "devices" && tc.upstream != nil {
				reply := providertest.Decode[gossiprpc.GoveeDevicesReply](t, res)
				assert.Equal(t, tc.wantKeyRejected, reply.KeyRejected)
			}
			assert.Equal(t, tc.wantHits, upstream.Hits())
		})
	}
}

const dynamicScenesBody = `{"requestId":"r","msg":"success","code":200,"payload":{"sku":"H6159","device":"AB:CD:EF","capabilities":[
	{"type":"devices.capabilities.dynamic_scene","instance":"lightScene","parameters":{"dataType":"ENUM","options":[
		{"name":"Sunrise","value":{"paramId":4280,"id":3853}},
		{"name":"Broken","value":"nope"}
	]}}
]}}`

const diyScenesBody = `{"requestId":"r","msg":"success","code":200,"payload":{"sku":"H6159","device":"AB:CD:EF","capabilities":[
	{"type":"devices.capabilities.diy_color_setting","instance":"diyScene","parameters":{"dataType":"ENUM","options":[
		{"name":"Xmas","value":8216930}
	]}}
]}}`

const listedScenesBody = `{"code":200,"message":"success","data":[
	{"sku":"H6159","device":"AB:CD:EF","capabilities":[
		{"type":"devices.capabilities.on_off","instance":"powerSwitch","parameters":{"dataType":"ENUM","options":[{"name":"on","value":1}]}},
		{"type":"devices.capabilities.dynamic_scene","instance":"lightScene","parameters":{"dataType":"ENUM","options":[{"name":"Tudum","value":3054}]}},
		{"type":"devices.capabilities.dynamic_scene","instance":"diyScene","parameters":{"dataType":"ENUM","options":[{"name":"Xmas","value":8216930}]}},
		{"type":"devices.capabilities.dynamic_scene","instance":"snapshot","parameters":{"dataType":"ENUM","options":[{"name":"Evening","value":0}]}}
	]},
	{"sku":"H5081","device":"11:22:33","capabilities":[
		{"type":"devices.capabilities.dynamic_scene","instance":"lightScene","parameters":{"dataType":"ENUM","options":[{"name":"Other device","value":1}]}}
	]}
]}`

type sceneUpstream struct {
	mu       sync.Mutex
	hits     map[string]int
	bodies   map[string]map[string]any
	dynamic  providertest.Reply
	diy      providertest.Reply
	listed   providertest.Reply
	gotKeys  []string
	testingT testing.TB
}

func newSceneUpstream(t testing.TB) *sceneUpstream {
	return &sceneUpstream{
		hits:     map[string]int{},
		bodies:   map[string]map[string]any{},
		dynamic:  providertest.Reply{Body: dynamicScenesBody},
		diy:      providertest.Reply{Body: diyScenesBody},
		listed:   providertest.Reply{Body: listedScenesBody},
		testingT: t,
	}
}

func (u *sceneUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reply := u.replyFor(r)
	if reply.Status != 0 {
		w.WriteHeader(reply.Status)
	}
	_, _ = io.WriteString(w, reply.Body)
}

func (u *sceneUpstream) replyFor(r *http.Request) providertest.Reply {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits[r.URL.Path]++
	u.gotKeys = append(u.gotKeys, r.Header.Get("Govee-API-Key"))
	switch r.URL.Path {
	case "/router/api/v1/device/scenes":
		u.recordBody(r)
		return u.dynamic
	case "/router/api/v1/device/diy-scenes":
		u.recordBody(r)
		return u.diy
	case "/router/api/v1/user/devices":
		return u.listed
	}
	u.testingT.Errorf("unexpected upstream call: %s %s", r.Method, r.URL.Path)
	return providertest.Reply{Status: http.StatusTeapot}
}

func (u *sceneUpstream) recordBody(r *http.Request) {
	assert.Equal(u.testingT, http.MethodPost, r.Method)
	b, _ := io.ReadAll(r.Body)
	var body map[string]any
	assert.NoError(u.testingT, codec.Unmarshal(b, &body))
	u.bodies[r.URL.Path] = body
}

func sceneRequest() gossiprpc.Request {
	return gossiprpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159"}
}

func TestScenesMergeEveryListForThatDeviceOnly(t *testing.T) {
	upstream := newSceneUpstream(t)
	p := newProvider(t, fakeKeys{key: "k-1"}, upstream)

	reply := providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", sceneRequest())

	assert.Empty(t, reply.Error)
	assert.Equal(t, []gossiprpc.GoveeScene{
		{ID: "lightScene:4280:3853", Name: "Sunrise", Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`{"paramId":4280,"id":3853}`)},
		{ID: "diyScene:8216930", Name: "Xmas", Type: "devices.capabilities.diy_color_setting", Instance: "diyScene", Value: codec.RawMessage(`8216930`)},
		{ID: "lightScene:3054", Name: "Tudum", Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`3054`)},
		{ID: "snapshot:0", Name: "Evening", Type: "devices.capabilities.dynamic_scene", Instance: "snapshot", Value: codec.RawMessage(`0`)},
	}, reply.Scenes)
	assert.Equal(t, map[string]any{"sku": "H6159", "device": "AB:CD:EF"}, upstream.bodies["/router/api/v1/device/scenes"]["payload"])
	assert.Equal(t, map[string]any{"sku": "H6159", "device": "AB:CD:EF"}, upstream.bodies["/router/api/v1/device/diy-scenes"]["payload"])
	assert.Equal(t, []string{"k-1", "k-1", "k-1"}, upstream.gotKeys)
}

func TestScenesAreCachedPerDeviceAndFreshBypasses(t *testing.T) {
	upstream := newSceneUpstream(t)
	p := newProvider(t, fakeKeys{key: "k"}, upstream)
	const scenesPath = "/router/api/v1/device/scenes"

	providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", sceneRequest())
	providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", sceneRequest())
	require.Equal(t, 1, upstream.hits[scenesPath])

	other := sceneRequest()
	other.Device = "11:22:33"
	providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", other)
	require.Equal(t, 2, upstream.hits[scenesPath])

	fresh := sceneRequest()
	fresh.Fresh = true
	providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", fresh)
	assert.Equal(t, 3, upstream.hits[scenesPath])
}

func TestScenesTreatAnUnsupportedKindAsEmpty(t *testing.T) {
	upstream := newSceneUpstream(t)
	upstream.diy = providertest.Reply{Status: http.StatusBadRequest, Body: `{"message":"unsupported"}`}
	upstream.dynamic = providertest.Reply{Body: `{"code":404,"msg":"device not found"}`}
	p := newProvider(t, fakeKeys{key: "k"}, upstream)

	reply := providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", sceneRequest())

	assert.Empty(t, reply.Error)
	assert.Len(t, reply.Scenes, 3, "the device list still offers its own scenes")
}

func TestScenesFailures(t *testing.T) {
	unauthorized := providertest.Reply{Status: http.StatusUnauthorized, Body: `{}`}
	for _, tc := range []struct {
		name            string
		dynamic         providertest.Reply
		wantError       string
		wantKeyRejected bool
	}{
		{"a rejected key says so", unauthorized, "Govee did not accept this API key", true},
		{"a body level rejection says so", providertest.Reply{Body: `{"code":401,"msg":"unauthorized"}`}, "Govee did not accept this API key", true},
		{"an upstream failure stays generic", providertest.Reply{Status: http.StatusInternalServerError, Body: `{}`}, "scene lookup failed", false},
		{"a rate limit is named", providertest.Reply{Status: http.StatusTooManyRequests, Body: `{}`}, "rate limiting", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := newSceneUpstream(t)
			upstream.dynamic = tc.dynamic
			p := newProvider(t, fakeKeys{key: "k"}, upstream)

			reply := providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", sceneRequest())

			assert.Contains(t, reply.Error, tc.wantError)
			assert.Equal(t, tc.wantKeyRejected, reply.KeyRejected)
			assert.Empty(t, reply.Scenes)
		})
	}
}

func TestScenesThatCannotBeServedDoNotDial(t *testing.T) {
	for _, tc := range []struct {
		name      string
		keys      fakeKeys
		req       gossiprpc.Request
		wantError string
	}{
		{"no channel", fakeKeys{key: "k"}, gossiprpc.Request{Device: "AB", SKU: "H"}, "missing channel"},
		{"no device", fakeKeys{key: "k"}, gossiprpc.Request{ChannelID: "2", SKU: "H"}, "missing device"},
		{"no sku", fakeKeys{key: "k"}, gossiprpc.Request{ChannelID: "2", Device: "AB"}, "missing device"},
		{"no key on file", fakeKeys{}, sceneRequest(), "no Govee API key"},
		{"key store failure", fakeKeys{err: errors.New("custody unreachable")}, sceneRequest(), "could not read your Govee key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.NewSequence(t)
			p := newProvider(t, tc.keys, upstream)

			reply := providertest.Call[gossiprpc.GoveeScenesReply](t, p, "scenes", tc.req)

			assert.Contains(t, reply.Error, tc.wantError)
			assert.Zero(t, upstream.Hits())
		})
	}
}

func recordControl(t *testing.T, status int, body string) (http.Handler, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/router/api/v1/device/control", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		assert.NoError(t, codec.Unmarshal(b, &decoded))
		mu.Lock()
		bodies = append(bodies, decoded)
		mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}), &bodies
}

func TestControlPlaysAScene(t *testing.T) {
	for _, tc := range []struct {
		name      string
		scene     gossiprpc.GoveeScene
		wantValue any
	}{
		{"dynamic pair", gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`{"paramId":4280,"id":3853}`)},
			map[string]any{"paramId": float64(4280), "id": float64(3853)}},
		{"static number", gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`3054`)}, float64(3054)},
		{"diy", gossiprpc.GoveeScene{Type: "devices.capabilities.diy_color_setting", Instance: "diyScene", Value: codec.RawMessage(`8216930`)}, float64(8216930)},
		{"snapshot zero", gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "snapshot", Value: codec.RawMessage(`0`)}, float64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, bodies := recordControl(t, 0, `{"code":200,"msg":"success"}`)
			p := newProvider(t, fakeKeys{key: "k"}, handler)
			req := sceneRequest()
			req.Scene = &tc.scene

			reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)

			require.True(t, reply.OK, reply.Error)
			require.Len(t, *bodies, 2, "control is power-on then scene")
			assert.Equal(t, map[string]any{"type": "devices.capabilities.on_off", "instance": "powerSwitch", "value": float64(1)}, capabilityOf(t, (*bodies)[0]))
			assert.Equal(t, map[string]any{"type": tc.scene.Type, "instance": tc.scene.Instance, "value": tc.wantValue}, capabilityOf(t, (*bodies)[1]))
		})
	}
}

func TestControlSendsOnlyTheParsedSceneValue(t *testing.T) {
	handler, bodies := recordControl(t, 0, `{"code":200}`)
	p := newProvider(t, fakeKeys{key: "k"}, handler)
	req := sceneRequest()
	req.Scene = &gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`{"paramId":1,"id":2,"extra":"x"}`)}

	reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)

	require.True(t, reply.OK, reply.Error)
	assert.Equal(t, map[string]any{"paramId": float64(1), "id": float64(2)}, capabilityOf(t, (*bodies)[1])["value"])
}

func TestControlRefusesABadSceneWithoutDialling(t *testing.T) {
	lightScene := func(value string) *gossiprpc.GoveeScene {
		return &gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(value)}
	}
	for _, tc := range []struct {
		name      string
		mutate    func(*gossiprpc.Request)
		wantError string
	}{
		{"unknown capability", func(r *gossiprpc.Request) {
			r.Scene = &gossiprpc.GoveeScene{Type: "devices.capabilities.on_off", Instance: "powerSwitch", Value: codec.RawMessage(`1`)}
		}, "invalid scene"},
		{"diy type on a light scene", func(r *gossiprpc.Request) {
			r.Scene = &gossiprpc.GoveeScene{Type: "devices.capabilities.diy_color_setting", Instance: "lightScene", Value: codec.RawMessage(`1`)}
		}, "invalid scene"},
		{"missing value", func(r *gossiprpc.Request) { r.Scene = lightScene(``) }, "invalid scene"},
		{"null value", func(r *gossiprpc.Request) { r.Scene = lightScene(`null`) }, "invalid scene"},
		{"negative value", func(r *gossiprpc.Request) { r.Scene = lightScene(`-1`) }, "invalid scene"},
		{"string value", func(r *gossiprpc.Request) { r.Scene = lightScene(`"Party"`) }, "invalid scene"},
		{"half a pair", func(r *gossiprpc.Request) { r.Scene = lightScene(`{"paramId":1}`) }, "invalid scene"},
		{"scene with power off", func(r *gossiprpc.Request) {
			r.Scene = lightScene(`1`)
			r.PowerOff = true
		}, "cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.NewSequence(t)
			p := newProvider(t, fakeKeys{key: "k"}, upstream)
			req := sceneRequest()
			tc.mutate(&req)

			reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)

			assert.False(t, reply.OK)
			assert.Contains(t, reply.Error, tc.wantError)
			assert.Zero(t, upstream.Hits())
		})
	}
}

func TestControlNamesASceneTheLightRefuses(t *testing.T) {
	upstream := providertest.NewSequence(t,
		providertest.Reply{Body: `{"code":200}`},
		providertest.Reply{Status: http.StatusBadRequest, Body: `{"message":"Parameter value out of range"}`})
	p := newProvider(t, fakeKeys{key: "k"}, upstream)
	req := sceneRequest()
	req.Scene = &gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`99`)}

	reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)

	assert.False(t, reply.OK)
	assert.Equal(t, "this light does not have that scene", reply.Error)
}

func TestControlRateLimitsScenes(t *testing.T) {
	upstream := providertest.NewSequence(t, providertest.Reply{Status: http.StatusTooManyRequests, Body: `{}`})
	p := newProvider(t, fakeKeys{key: "k"}, upstream)
	req := sceneRequest()
	req.Scene = &gossiprpc.GoveeScene{Type: "devices.capabilities.dynamic_scene", Instance: "lightScene", Value: codec.RawMessage(`1`)}

	reply := providertest.Call[gossiprpc.GoveeControlReply](t, p, "control", req)

	assert.Equal(t, "too many light changes, slow down", reply.Error)
}

func capabilityOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	payload, ok := body["payload"].(map[string]any)
	require.True(t, ok, "body has payload")
	capability, ok := payload["capability"].(map[string]any)
	require.True(t, ok, "payload has capability")
	return capability
}
