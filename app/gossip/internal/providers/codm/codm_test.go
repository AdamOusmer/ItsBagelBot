// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codm

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
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
	"ItsBagelBot/pkg/ratelimit"
	"ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkeygo "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func init() { core.SetSSRFCheckForTests(false) }

type fakeSOCKS struct {
	ln net.Listener
	wg sync.WaitGroup
}

func newFakeSOCKS(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	previous := core.WARPProxyAddr()
	core.SetWARPProxyAddrForTests(ln.Addr().String())
	f := &fakeSOCKS{ln: ln}
	f.wg.Add(1)
	go f.serve()
	t.Cleanup(func() {
		core.SetWARPProxyAddrForTests(previous)
		_ = ln.Close()
		f.wg.Wait()
	})
}

func (f *fakeSOCKS) serve() {
	defer f.wg.Done()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			f.handle(conn)
		}()
	}
}

func (f *fakeSOCKS) handle(conn net.Conn) {
	if err := socksHandshake(conn); err != nil {
		return
	}
	host, err := socksHost(conn)
	if err != nil {
		return
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))))
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(target, conn) }()
	_, _ = io.Copy(conn, target)
}

func socksHandshake(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 5 {
		return errors.New("invalid SOCKS version")
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	_, err := conn.Write([]byte{5, 0})
	return err
}

func socksHost(conn net.Conn) (string, error) {
	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return "", err
	}
	if request[0] != 5 {
		return "", errors.New("invalid SOCKS version")
	}
	if request[1] != 1 {
		return "", errors.New("invalid SOCKS command")
	}
	switch request[3] {
	case 1:
		return socksIP(conn, 4)
	case 4:
		return socksIP(conn, 16)
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		name := make([]byte, length[0])
		_, err := io.ReadFull(conn, name)
		return string(name), err
	default:
		return "", errors.New("invalid SOCKS address type")
	}
}

func socksIP(conn net.Conn, size int) (string, error) {
	ip := make([]byte, size)
	_, err := io.ReadFull(conn, ip)
	return net.IP(ip).String(), err
}

type memStore struct {
	mu   sync.Mutex
	m    map[string][]byte
	ttls map[string]time.Duration
}

func newMemStore() *memStore {
	return &memStore{m: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return append([]byte(nil), b...), ok, nil
}

func (s *memStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	s.ttls[key] = ttl
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

func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func endpoint(t *testing.T, p provider.Provider) func(context.Context, gossiprpc.Request) any {
	t.Helper()
	for _, ep := range p.Endpoints() {
		if ep.Name == "profile" {
			return ep.Handle
		}
	}
	t.Fatal("profile endpoint not declared")
	return nil
}

func replyOf[T any](t *testing.T, value any) T {
	t.Helper()
	if reply, ok := value.(T); ok {
		return reply
	}
	raw, ok := value.(codec.RawMessage)
	require.True(t, ok, "unexpected handler result %T", value)
	var reply T
	require.NoError(t, codec.Unmarshal(raw, &reply))
	return reply
}

func TestProfileRedirectsAndPreservesExactIDs(t *testing.T) {
	newFakeSOCKS(t)
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/validate", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, codec.Unmarshal(body, &got))
		require.Len(t, got, 5)
		for _, key := range []string{"country", "voucherTypeName", "whiteLabelId", "deviceId", "userId"} {
			require.Contains(t, got, key)
		}
		mu.Lock()
		bodies = append(bodies, got)
		mu.Unlock()
		if got["country"] == "IN" {
			_, _ = io.WriteString(w, `{"success":false,"errorCode":-200,"errorMsg":"country mismatch","homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":{"type":"SUCCESS","result":0,"countryId":124,"level":414,"nickname":"StreamerMode","rankClass":21,"customReadableMpRank":"Master I","rating":4590,"shortId":"TEST01"}}`)
	}))
	defer srv.Close()

	store := newMemStore()
	p := New(Config{BaseURL: srv.URL, Country: "IN"}, provider.Deps{
		Cache: core.NewCache(store),
		Log:   zap.NewNop(),
	})
	h := endpoint(t, p)
	ids := []string{"7000000000000000000", "  Élite玩家  "}
	for _, id := range ids {
		got := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: id}))
		wantPlayer := strings.TrimSpace(id)
		assert.Equal(t, gossiprpc.CODMProfileReply{
			Player: wantPlayer, Level: 414, Rank: "Master I", RankClass: 21,
			Rating: 4590, Country: "CA", ShortID: "TEST01",
		}, got)
		assert.NotEqual(t, "StreamerMode", got.Player)
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 4)
	for i, id := range ids {
		initial := bodies[i*2]
		redirected := bodies[i*2+1]
		wantPlayer := strings.TrimSpace(id)
		assert.Equal(t, "IN", initial["country"])
		assert.Equal(t, "CA", redirected["country"])
		assert.Equal(t, wantPlayer, initial["userId"])
		assert.Equal(t, wantPlayer, redirected["userId"])
		assert.Equal(t, "CALL_OF_DUTY_MOBILE_WL", initial["voucherTypeName"])
		assert.Equal(t, "1", initial["whiteLabelId"])
		assert.NotEmpty(t, initial["deviceId"])
		assert.Equal(t, initial["deviceId"], redirected["deviceId"])
	}
}

func TestProfileNeverFollowsHTTPRedirects(t *testing.T) {
	newFakeSOCKS(t)
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var forbiddenCalls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/validate" {
					forbiddenCalls.Add(1)
				}
				http.Redirect(w, r, "/purchase", status)
			}))
			defer srv.Close()
			p := New(Config{BaseURL: srv.URL}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
			got := replyOf[gossiprpc.CODMProfileReply](t, endpoint(t, p)(context.Background(), gossiprpc.Request{Account: "7081192462291238913"}))
			require.Equal(t, "profile lookup failed", got.Error)
			assert.Zero(t, forbiddenCalls.Load())
		})
	}
}

func TestProfileCacheIsCaseSensitiveAndUsesFiveMinuteFreshTTL(t *testing.T) {
	newFakeSOCKS(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"success":true,"result":{"type":"SUCCESS","result":0,"countryId":124,"level":1,"nickname":"StreamerMode","rankClass":1,"customReadableMpRank":"Rookie I","rating":10,"shortId":"TEST01"}}`)
	}))
	defer srv.Close()

	store := newMemStore()
	h := endpoint(t, New(Config{BaseURL: srv.URL}, provider.Deps{Cache: core.NewCache(store), Log: zap.NewNop()}))
	first := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "CaseID"}))
	second := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "CaseID"}))
	third := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "caseid"}))

	assert.Equal(t, first, second)
	assert.Equal(t, "CaseID", first.Player)
	assert.Equal(t, "caseid", third.Player)
	assert.Equal(t, first.Level, third.Level)
	assert.Equal(t, first.Rank, third.Rank)
	assert.Equal(t, first.RankClass, third.RankClass)
	assert.Equal(t, first.Rating, third.Rating)
	assert.Equal(t, first.Country, third.Country)
	assert.Equal(t, first.ShortID, third.ShortID)
	assert.Equal(t, 2, calls, "different account case must not share the cache entry")
	assert.Equal(t, 10*time.Minute, positiveTTL(store))
}

func TestProfileRejectsInvalidAndMalformedResponses(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantCalls int
	}{
		{name: "missing result", body: `{"success":true}`, wantCalls: 1},
		{name: "invalid nested profile", body: `{"result":{"type":"SUCCESS","result":0,"countryId":124}}`, wantCalls: 1},
		{name: "redirect without country", body: `{"success":false,"errorCode":-200}`, wantCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newFakeSOCKS(t)
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			h := endpoint(t, New(Config{BaseURL: srv.URL}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()}))
			got := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "valid"}))
			assert.Equal(t, "profile lookup failed", got.Error)
			assert.Equal(t, tc.wantCalls, calls)
		})
	}

	for _, account := range []string{"", "  ", "bad\nname", string([]byte{0xff}), strings.Repeat("x", maxAccountRunes+1)} {
		t.Run("invalid account", func(t *testing.T) {
			h := endpoint(t, New(Config{}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()}))
			got := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: account}))
			assert.Equal(t, "invalid account", got.Error)
		})
	}
}

func TestProfileRedirectLoopsAreBounded(t *testing.T) {
	newFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"IN"}`)
	}))
	defer srv.Close()
	h := endpoint(t, New(Config{BaseURL: srv.URL}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()}))
	got := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "loop"}))
	assert.Equal(t, "profile lookup failed", got.Error)
	assert.Equal(t, 2, calls, "redirecting back to the original country must stop immediately")

	calls = 0
	countries := []string{"CA", "GB", "AU", "JP", "BR"}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		country := countries[calls-1]
		_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"`+country+`"}`)
	})
	got = replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "bounded"}))
	assert.Equal(t, "profile lookup failed", got.Error)
	assert.Equal(t, maxRedirects+1, calls, "country redirects must have a hard bound")
}

func TestProfileMapsUpstream429AndDoesNotRefetchDuringThrottleCache(t *testing.T) {
	newFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"too many requests"}`)
	}))
	defer srv.Close()
	store := newMemStore()
	d := provider.Deps{Cache: core.NewCache(store), Log: zap.NewNop()}
	h := endpoint(t, New(Config{BaseURL: srv.URL}, d))

	first := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "busy"}))
	second := replyOf[gossiprpc.CODMProfileReply](t, h(context.Background(), gossiprpc.Request{Account: "busy"}))
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", first.Error)
	assert.Equal(t, first, second)
	otherReplica := endpoint(t, New(Config{BaseURL: srv.URL}, d))
	other := replyOf[gossiprpc.CODMProfileReply](t, otherReplica(context.Background(), gossiprpc.Request{Account: "different-player"}))
	assert.Equal(t, first.Error, other.Error)
	assert.Equal(t, 90*time.Second, store.ttls[cooldownKey])
	assert.Equal(t, 1, calls)
}

func TestLocalDenialDoesNotArmCooldown(t *testing.T) {
	store := newMemStore()
	d := provider.Deps{Cache: core.NewCache(store), Log: zap.NewNop()}
	p := newAPI(Config{}, d, provider.NewProvider(providerName, d))
	p.recordThrottle(context.Background(), &core.UpstreamError{Status: 429, LocalDeny: true})
	_, found, err := store.Get(context.Background(), cooldownKey)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestValidationRateAccountingIntegration(t *testing.T) {
	newFakeSOCKS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request validateRequest
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, codec.Unmarshal(body, &request))
		if request.Country == "IN" {
			_, _ = io.WriteString(w, `{"errorCode":-200,"homeBaseCountry2Name":"CA"}`)
			return
		}
		_, _ = io.WriteString(w, `{"result":{"type":"SUCCESS","result":0,"countryId":124,"level":414,"nickname":"StreamerMode","rankClass":21,"customReadableMpRank":"Master I","rating":4590}}`)
	}))
	defer srv.Close()
	p, client := rateTestAPI(t, srv.URL)
	ctx := context.Background()
	_, err := p.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "first"})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "one country redirect sends exactly two validations")
	raw, err := client.Do(ctx, client.B().Hget().Key(rateTestHTTPKey(p)).Field("tokens").Build()).ToString()
	require.NoError(t, err)
	tokens, err := strconv.ParseFloat(raw, 64)
	require.NoError(t, err)
	assert.InDelta(t, 2.0, tokens, 0.5, "only two of the four burst tokens were spent")
	otherReplica := *p
	_, err = otherReplica.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "second"})
	require.NoError(t, err)
	_, err = p.fetchProfile(ctx, gossiprpc.Request{}, provider.ID{Display: "third"})
	var denial *core.UpstreamError
	require.ErrorAs(t, err, &denial)
	assert.True(t, denial.LocalDeny)
	assert.Equal(t, 4, calls, "replicas share the HTTP burst and a denial sends nothing")
}

func TestRateAdmissionPreservesPremiumReserveIntegration(t *testing.T) {
	p, client := rateTestAPI(t, "https://example.test")
	ctx := context.Background()
	key := "test:codm:" + p.deviceID + ":lookups:standard"
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	err := client.Do(ctx, client.B().Hset().Key(key).FieldValue().
		FieldValue("tokens", "0").FieldValue("last_ms", future).Build()).Error()
	require.NoError(t, err)
	var denial *core.UpstreamError
	require.ErrorAs(t, p.admit(ctx, gossiprpc.Request{}), &denial)
	assert.True(t, denial.LocalDeny)
	require.NoError(t, p.admit(ctx, gossiprpc.Request{IsPremium: true}))
	require.NoError(t, p.spendValidation(ctx), "actual HTTP budget is independent of the flight winner's lane")
}

func rateTestAPI(t *testing.T, base string) (*api, valkeygo.Client) {
	t.Helper()
	address := os.Getenv("VALKEY_TEST_ADDR")
	if address == "" {
		t.Skip("VALKEY_TEST_ADDR is not set")
	}
	client, err := valkey.NewClient(address, os.Getenv("VALKEY_TEST_PASSWORD"))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	d := provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop(), Limiter: ratelimit.New(client)}
	p := newAPI(Config{BaseURL: base}, d, provider.NewProvider(providerName, d))
	p.deviceID = uuid.NewString()
	p.buckets = p.buckets.WithKey("test:codm:" + p.deviceID + ":lookups")
	p.requests = p.requests.WithKey(rateTestHTTPKey(p))
	return p, client
}

func rateTestHTTPKey(p *api) string { return "test:codm:" + p.deviceID + ":http" }

func TestNewUsesProtectedDefaultWARPClient(t *testing.T) {
	b := provider.NewProvider(providerName, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()})
	p := newAPI(Config{}, provider.Deps{Cache: core.NewCache(newMemStore()), Log: zap.NewNop()}, b)
	assert.Equal(t, core.LaneWARP, p.http.Lane())
}

func positiveTTL(s *memStore) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, ttl := range s.ttls {
		if len(s.m[key]) > 0 && ttl > 0 {
			return ttl
		}
	}
	return 0
}
