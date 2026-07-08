package govee

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// fakeKeys is a canned key resolver: key by broadcaster id, err short-circuits.
type fakeKeys struct {
	key string
	err error
}

func (f fakeKeys) Key(context.Context, string) (string, error) { return f.key, f.err }

func newTestProvider(t *testing.T, keys provider.GoveeKeyResolver, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL},
		provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop(), GoveeKeys: keys})
}

func endpoint(t *testing.T, p *Provider, name string) func(context.Context, gatewayrpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == name {
			return ep.Handle
		}
	}
	t.Fatalf("endpoint %q not declared", name)
	return nil
}

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
	p := newTestProvider(t, fakeKeys{key: "k-123"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/router/api/v1/user/devices", r.URL.Path)
		gotKey = r.Header.Get("Govee-API-Key")
		_, _ = io.WriteString(w, deviceListBody)
	}))

	reply := asReply[gatewayrpc.GoveeDevicesReply](t, endpoint(t, p, "devices")(context.Background(), gatewayrpc.Request{ChannelID: "2"}))
	assert.Equal(t, "k-123", gotKey, "the broadcaster's key must ride the header")
	require.Len(t, reply.Devices, 2)
	assert.Equal(t, "AB:CD:EF", reply.Devices[0].Device)
	assert.Equal(t, "H6159", reply.Devices[0].SKU)
	assert.Equal(t, "Desk strip", reply.Devices[0].Name)
	assert.True(t, reply.Devices[0].Color, "colour-capable device flagged")
	assert.False(t, reply.Devices[1].Color, "plug without colour not flagged")
}

func TestDevicesNoKeyOnFile(t *testing.T) {
	called := false
	p := newTestProvider(t, fakeKeys{key: ""}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	reply := asReply[gatewayrpc.GoveeDevicesReply](t, endpoint(t, p, "devices")(context.Background(), gatewayrpc.Request{ChannelID: "2"}))
	assert.Contains(t, reply.Error, "no Govee API key")
	assert.False(t, called, "must not dial Govee with no key")
}

func TestControlPowersOnThenSetsColor(t *testing.T) {
	var bodies []map[string]any
	var gotKey string
	var mu sync.Mutex
	p := newTestProvider(t, fakeKeys{key: "k-9"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/router/api/v1/device/control", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, json.Unmarshal(b, &body))
		mu.Lock()
		gotKey = r.Header.Get("Govee-API-Key")
		bodies = append(bodies, body)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"code":200,"message":"success"}`)
	}))

	reply := asReply[gatewayrpc.GoveeControlReply](t, endpoint(t, p, "control")(context.Background(),
		gatewayrpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0x00CCFF}))

	require.True(t, reply.OK)
	assert.Equal(t, "k-9", gotKey)
	require.Len(t, bodies, 2, "control is power-on then colour")

	power := capabilityOf(t, bodies[0])
	assert.Equal(t, "devices.capabilities.on_off", power["type"])
	assert.Equal(t, "powerSwitch", power["instance"])
	assert.EqualValues(t, 1, power["value"])

	color := capabilityOf(t, bodies[1])
	assert.Equal(t, "devices.capabilities.color_setting", color["type"])
	assert.Equal(t, "colorRgb", color["instance"])
	assert.EqualValues(t, 0x00CCFF, color["value"])
}

func TestControlAPILevelFailure(t *testing.T) {
	p := newTestProvider(t, fakeKeys{key: "k"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTP 200 but an API-level failure code in the body.
		_, _ = io.WriteString(w, `{"code":400,"message":"invalid device"}`)
	}))
	reply := asReply[gatewayrpc.GoveeControlReply](t, endpoint(t, p, "control")(context.Background(),
		gatewayrpc.Request{ChannelID: "2", Device: "AB:CD:EF", SKU: "H6159", ColorRGB: 0xFF0000}))
	assert.False(t, reply.OK)
	assert.NotEmpty(t, reply.Error)
}

func TestControlMissingDevice(t *testing.T) {
	p := newTestProvider(t, fakeKeys{key: "k"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("must not dial Govee without a device")
	}))
	reply := asReply[gatewayrpc.GoveeControlReply](t, endpoint(t, p, "control")(context.Background(),
		gatewayrpc.Request{ChannelID: "2", SKU: "H6159", ColorRGB: 1}))
	assert.Contains(t, reply.Error, "missing device")
}

// capabilityOf digs the capability object out of a control request body.
func capabilityOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	payload, ok := body["payload"].(map[string]any)
	require.True(t, ok, "body has payload")
	capability, ok := payload["capability"].(map[string]any)
	require.True(t, ok, "payload has capability")
	return capability
}
