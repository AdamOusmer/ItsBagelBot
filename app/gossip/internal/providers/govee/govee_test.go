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
		name      string
		endpoint  string
		keys      fakeKeys
		req       gossiprpc.Request
		upstream  []providertest.Reply
		wantError string
		wantHits  int
	}{
		{"devices: rejects a request without a channel", "devices", fakeKeys{key: "k"},
			gossiprpc.Request{}, nil, "missing channel", 0},
		{"devices: does not dial Govee with no key on file", "devices", fakeKeys{},
			gossiprpc.Request{ChannelID: channel}, nil, "no Govee API key", 0},
		{"devices: reports a key store failure", "devices", fakeKeys{err: errors.New("custody unreachable")},
			gossiprpc.Request{ChannelID: channel}, nil, "could not read your Govee key", 0},
		{"devices: reports an upstream failure", "devices", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel}, []providertest.Reply{{Status: http.StatusInternalServerError, Body: `{}`}}, "device lookup failed", 1},
		{"control: does not dial Govee without a device", "control", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel, SKU: "H6159", ColorRGB: 1}, nil, "missing device", 0},
		{"control: surfaces an API level failure", "control", fakeKeys{key: "k"},
			gossiprpc.Request{ChannelID: channel, Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0xFF0000},
			[]providertest.Reply{{Body: `{"code":400,"message":"invalid device"}`}}, "could not reach your lights", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := providertest.NewSequence(t, tc.upstream...)
			p := newProvider(t, tc.keys, upstream)

			res := providertest.Endpoint(t, p, tc.endpoint)(context.Background(), tc.req)

			assert.Contains(t, providertest.ErrorOf(t, res), tc.wantError)
			assert.Equal(t, tc.wantHits, upstream.Hits())
		})
	}
}

func capabilityOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	payload, ok := body["payload"].(map[string]any)
	require.True(t, ok, "body has payload")
	capability, ok := payload["capability"].(map[string]any)
	require.True(t, ok, "payload has capability")
	return capability
}
