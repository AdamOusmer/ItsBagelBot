// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The provider's client rides the WARP lane — the whole point of the inverted
// default — so e2e tests stage a minimal SOCKS5 forwarder on loopback and aim
// warpProxyAddr at it. CONNECT targets arrive as hostnames (remote-DNS form)
// and are dialed directly here, which is what Cloudflare's edge does in
// production. The SSRF gate stays off for these plain-http fakes EXCEPT in
// TestFetchDeniedBySSRFGate, which flips it back on to pin the denial
// end-to-end; the gate's full semantics live in core's table tests.

func init() { core.SetSSRFCheckForTests(false) }

// fakeSOCKS is a single-listener SOCKS5 CONNECT forwarder.
type fakeSOCKS struct {
	ln net.Listener
	wg sync.WaitGroup

	mu       sync.Mutex
	refusing bool // fail every CONNECT (staged tunnel-down / dead origin)

	conns atomic.Int32
}

func newFakeSOCKS(t *testing.T) *fakeSOCKS {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeSOCKS{ln: ln}
	prev := core.WARPProxyAddr()
	core.SetWARPProxyAddrForTests(ln.Addr().String())
	t.Cleanup(func() {
		core.SetWARPProxyAddrForTests(prev)
		_ = ln.Close()
		f.wg.Wait()
	})
	f.wg.Add(1)
	go f.serve()
	return f
}

func (f *fakeSOCKS) setRefusing(v bool) {
	f.mu.Lock()
	f.refusing = v
	f.mu.Unlock()
}

func (f *fakeSOCKS) serve() {
	defer f.wg.Done()
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer conn.Close()
			f.handle(conn)
		}()
	}
}

func (f *fakeSOCKS) handle(conn net.Conn) {
	if !socksGreet(conn) {
		return
	}
	target, ok := readSOCKSTarget(conn)
	if !ok {
		return
	}
	f.pipe(conn, target)
}

// socksGreet answers the VER NMETHODS METHODS greeting with "no auth".
func socksGreet(conn net.Conn) bool {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 5 {
		return false
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return false
	}
	_, err := conn.Write([]byte{5, 0})
	return err == nil
}

// readSOCKSTarget decodes the VER CMD RSV ATYP ADDR PORT request (CONNECT
// only) into a dialable host:port.
func readSOCKSTarget(conn net.Conn) (string, bool) {
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil || req[1] != 1 {
		return "", false
	}
	host, ok := readSOCKSHost(conn, req[3])
	if !ok {
		return "", false
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(port)))), true
}

func readSOCKSHost(conn net.Conn, atyp byte) (string, bool) {
	switch atyp {
	case 1:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(conn, ip); err != nil {
			return "", false
		}
		return net.IP(ip).String(), true
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return "", false
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return "", false
		}
		return string(name), true
	}
	return "", false
}

// pipe answers the CONNECT: refused when the fake is refusing or the dial
// fails, otherwise splices both directions until either side closes.
func (f *fakeSOCKS) pipe(conn net.Conn, target string) {
	f.mu.Lock()
	refuse := f.refusing
	f.mu.Unlock()
	upstream, err := net.Dial("tcp", target)
	if refuse && err == nil {
		_ = upstream.Close()
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}) // reply: refused
		return
	}
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	f.conns.Add(1)
	_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go func() { _, _ = io.Copy(upstream, conn); _ = upstream.(*net.TCPConn).CloseWrite() }()
	_, _ = io.Copy(conn, upstream)
	_ = upstream.Close()
}

// --- harness ------------------------------------------------------------------

type memStore struct {
	mu   sync.Mutex
	m    map[string][]byte
	ttls map[string]time.Duration
}

func newMemStore() *memStore {
	return &memStore{m: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (s *memStore) retention(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ttls[key]
}

func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.m[key]
	return b, ok, nil
}

func (s *memStore) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), val...)
	s.ttls[key] = ttl
	return nil
}

func (s *memStore) Del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memStore) SetNX(_ context.Context, key string, val []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = append([]byte(nil), val...)
	s.ttls[key] = ttl
	return true, nil
}

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
	store *memStore
	socks *fakeSOCKS

	srv   *httptest.Server
	hits  atomic.Int32 // upstream requests served
	admit atomic.Int32 // budget spends admitted

	routesMu sync.Mutex
	routes   map[string]staged

	defsMu sync.Mutex
	defs   map[string]gossiprpc.FetchDef
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		store:  newMemStore(),
		socks:  newFakeSOCKS(t),
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
	b := provider.NewProvider(providerName, deps) // no .Trusted(): WARP lane by default
	cfg := Config{ChannelRateLimit: 6, DefRateLimit: 30, HostRateLimit: 120, PositiveTTL: time.Minute}
	h.p = newAPI(cfg, deps, b)
	h.p.admit = func(context.Context, *flight, bool) error {
		h.admit.Add(1)
		return nil
	}
	return h
}

// route stages (or restages) one upstream response for an exact path. Tests
// may call it again mid-flight to change what the upstream says.
func (h *harness) route(t *testing.T, path string, r staged) {
	t.Helper()
	h.routesMu.Lock()
	h.routes[path] = r
	h.routesMu.Unlock()
}

// addDef registers a stored definition pointing at the harness upstream.
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

// --- extraction ----------------------------------------------------------------

func TestFetchExtractsNestedJSONPath(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json",
		body: `{"data":{"items":[{"name":"Shiny Thing"},{"name":"Other"}]},"n":42,"ok":true}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{
		URL:      "placeholder",
		IsActive: true,
		JSONPath: []string{"data", "items", "0", "name"},
	})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	assert.Equal(t, gossiprpc.FetchOK, reply.Status)
	require.Len(t, reply.Values, 1)
	assert.Equal(t, "Shiny Thing", reply.Values[0])
	assert.GreaterOrEqual(t, reply.MS, 0)
}

func TestFetchTokenTailOverridesStoredPath(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"a":"first","b":42,"c":true}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"a"}})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx.b"})
	assert.Equal(t, gossiprpc.FetchOK, reply.Status)
	require.Len(t, reply.Values, 1)
	assert.Equal(t, "42", reply.Values[0], "numbers coerce from their raw bytes, no float drift")

	reply = call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx.c"})
	assert.Equal(t, "true", reply.Values[0])
}

func TestFetchPlainKindReturnsBodyText(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/plain", staged{status: http.StatusOK, ct: "text/plain", body: "  hello from upstream\n"})
	h.addDef("plain", "/plain", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "plain"})
	assert.Equal(t, gossiprpc.FetchOK, reply.Status)
	assert.Equal(t, []string{"hello from upstream"}, reply.Values)
}

func TestFetchUnresolvablePathIsBadDefAndNegativeCached(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"a":"x"}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"nope"}})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	assert.Equal(t, gossiprpc.FetchBadDef, reply.Status)
	assert.Equal(t, int32(1), h.hits.Load())
	// Stable authoring state: negative-cached for negativeTTL (the byte entry
	// physically retains 2x its fresh window, the standard stale tail).
	assert.Equal(t, 2*negativeTTL, h.store.retention(resultKey("wx")))
	call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	assert.Equal(t, int32(1), h.hits.Load(), "second ask must come from the negative cache")
}

// --- caching -------------------------------------------------------------------

func TestFetchPositiveCachesThenFreshBypassesReadButWrites(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"v"}})

	first := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, first.Status)
	assert.Equal(t, int32(1), h.hits.Load())

	// Change what the upstream says, then ask FRESH.
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":2}`})

	cached := call(t, h, gossiprpc.Request{ChannelID: "ch2", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, cached.Status)
	assert.Equal(t, []string{"1"}, cached.Values, "normal read serves the stored entry")
	assert.Equal(t, int32(1), h.hits.Load())

	fresh := call(t, h, gossiprpc.Request{ChannelID: "ch2", DefID: "wx", Fresh: true})
	require.Equal(t, gossiprpc.FetchOK, fresh.Status)
	assert.Equal(t, []string{"2"}, fresh.Values, "fresh must skip the positive-cache read")
	assert.Equal(t, int32(2), h.hits.Load())

	again := call(t, h, gossiprpc.Request{ChannelID: "ch3", DefID: "wx"})
	assert.Equal(t, []string{"2"}, again.Values, "the fresh result must have been written back")
	assert.Equal(t, int32(2), h.hits.Load())
}

func TestFetchUpstream404NegativeCachedAt15s(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/gone", staged{status: http.StatusNotFound, ct: "application/json", body: `{"error":"nope"}`})
	h.addDef("gone", "/gone", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "gone"})
	assert.Equal(t, gossiprpc.FetchUpstreamError, reply.Status)
	assert.Equal(t, 2*negativeTTL, h.store.retention(resultKey("gone")))

	call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "gone"})
	assert.Equal(t, int32(1), h.hits.Load())
}

func TestFetchInfraFailureStaysUncached(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/boom", staged{status: http.StatusInternalServerError, ct: "text/plain", body: "dead"})
	h.addDef("boom", "/boom", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "boom"})
	assert.Equal(t, gossiprpc.FetchUpstreamError, reply.Status)
	_, found, err := h.store.Get(context.Background(), resultKey("boom"))
	require.NoError(t, err)
	assert.False(t, found, "infrastructure failures teach nothing; they must not be cached")

	call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "boom"})
	assert.Equal(t, int32(2), h.hits.Load(), "uncached means every ask re-dials")
}

// --- budgets and rehearsal ------------------------------------------------------

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
	_, found, err := h.store.Get(context.Background(), resultKey("wx"))
	require.NoError(t, err)
	assert.False(t, found, "dry runs write no cache")
}

func TestFetchBucketDenialAnswersLimited(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})
	denials := atomic.Int32{}
	h.p.admit = func(context.Context, *flight, bool) error {
		if denials.Add(1) > 0 {
			return &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true}
		}
		return nil
	}

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	assert.Equal(t, gossiprpc.FetchLimited, reply.Status)
	assert.Zero(t, h.hits.Load(), "a bucket denial never reaches the upstream")
	_, found, err := h.store.Get(context.Background(), resultKey("wx"))
	require.NoError(t, err)
	assert.False(t, found, "denials are retried on the next request, never pinned")
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

// --- breaker --------------------------------------------------------------------

func TestBreakerArmsAfterFiveConsecutiveTransportFailures(t *testing.T) {
	h := newHarness(t)
	// A def whose host the tunnel cannot reach: every attempt is a transport
	// failure (DNS/connect dies inside the tunnel), none of them answerable.
	// Set directly — addDef would rewrite the URL onto the fake upstream.
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
		"transport failure without an answer maps to timeout, the infra family sesame renders as [source timed out]")

	_, armed, err := h.store.Get(context.Background(), breakerKey("blackhole.invalid"))
	require.NoError(t, err)
	assert.True(t, armed, "five consecutive transport failures must arm the fleet-wide circuit")

	// Armed hosts answer limited WITHOUT dialing — even a healthy one.
	h.route(t, "/healthy", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
	h.addDef("healthy", "/healthy", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, KeyLabel: ""})
	// Same host? No — the armed key is host-scoped, so stage the healthy def on
	// the SAME host by pointing its URL through a def-level rewrite.
	h.defs["samehost"] = gossiprpc.FetchDef{
		Name:     "samehost",
		URL:      "https://blackhole.invalid/unreachable-but-armed",
		IsActive: true,
	}
	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "samehost"})
	assert.Equal(t, gossiprpc.FetchLimited, reply.Status, "armed host answers limited without dialing")

	// An ANSWERED request (even a 500) proves reachability and resets the
	// consecutive counter, so the failures before it stop counting.
	h2 := newHarness(t)
	host := hostOf(h2.srv.URL)
	h2.defs["flap"] = gossiprpc.FetchDef{Name: "flap", URL: "https://" + host + "/unreachable-host-route", IsActive: true}
	// Stage transport failures on that host by refusing them at the tunnel.
	h2.socks.setRefusing(true)
	for i := 0; i < breakerThreshold-1; i++ {
		call(t, h2, gossiprpc.Request{ChannelID: "ch1", DefID: "flap"})
	}
	_, armedPre, err := h2.store.Get(context.Background(), breakerKey(host))
	require.NoError(t, err)
	assert.False(t, armedPre, "%d failures alone must not arm", breakerThreshold-1)
	// Now an answered request on the same host: tunnel lets it through again.
	h2.socks.setRefusing(false)
	h2.route(t, "/alive", staged{status: http.StatusInternalServerError, ct: "text/plain", body: "answering, badly"})
	h2.defs["flap"] = gossiprpc.FetchDef{Name: "flap", URL: h2.srv.URL + "/alive", IsActive: true}
	call(t, h2, gossiprpc.Request{ChannelID: "ch1", DefID: "flap"})
	for i := 0; i < breakerThreshold-1; i++ {
		call(t, h2, gossiprpc.Request{ChannelID: "ch1", DefID: "flap"})
	}
	_, armedPost, err := h2.store.Get(context.Background(), breakerKey(host))
	require.NoError(t, err)
	assert.False(t, armedPost, "the reset in between means %d later failures stay under the threshold", breakerThreshold-1)
}

func hostOf(raw string) string {
	return strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")
}

// --- definition resolution -------------------------------------------------------

func TestFetchBadDefs(t *testing.T) {
	t.Run("missing def", func(t *testing.T) {
		h := newHarness(t)
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "ghost"})
		assert.Equal(t, gossiprpc.FetchBadDef, reply.Status)
	})
	t.Run("inactive def", func(t *testing.T) {
		h := newHarness(t)
		h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{}`})
		h.addDef("paused", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: false})
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "paused"})
		assert.Equal(t, gossiprpc.FetchBadDef, reply.Status)
		assert.Zero(t, h.hits.Load(), "inactive defs never dial")
	})
	t.Run("dangling key label fails closed", func(t *testing.T) {
		h := newHarness(t)
		h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: `{"v":1}`})
		h.addDef("keyed", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, KeyLabel: "gone"})
		reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "keyed"})
		assert.Equal(t, gossiprpc.FetchBadDef, reply.Status, "no resolver wired: fail closed, never send unauthenticated")
		assert.Zero(t, h.hits.Load())
	})
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

	_, found, err := h.store.Get(context.Background(), resultKey("unsaved"))
	require.NoError(t, err)
	assert.False(t, found, "inline drafts never touch the shared cache")
}

// --- gate and payload policy -----------------------------------------------------

func TestFetchDeniedBySSRFGate(t *testing.T) {
	core.SetSSRFCheckForTests(true) // this one test wants the real gate
	t.Cleanup(func() { core.SetSSRFCheckForTests(false) })

	h := newHarness(t)
	h.defs["meta"] = gossiprpc.FetchDef{
		Name:     "meta",
		URL:      "https://169.254.169.254/latest/meta-data/",
		IsActive: true,
	}
	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "meta"})
	assert.Equal(t, gossiprpc.FetchDenied, reply.Status)
}

func TestFetchRejectsDisallowedContentType(t *testing.T) {
	h := newHarness(t)
	h.route(t, "/binary", staged{status: http.StatusOK, ct: "application/octet-stream", body: "\xde\xad"})
	h.addDef("bin", "/binary", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "bin"})
	assert.Equal(t, gossiprpc.FetchUpstreamError, reply.Status)
}

func TestFetchCapsValues(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("x", 500)
	h.route(t, "/long", staged{status: http.StatusOK, ct: "text/plain", body: long})
	h.addDef("long", "/long", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "long"})
	require.Equal(t, gossiprpc.FetchOK, reply.Status)
	require.Len(t, reply.Values, 1)
	assert.LessOrEqual(t, len([]rune(reply.Values[0])), maxValueRunes)
}

// --- timeout classification ------------------------------------------------------

// The sample exists for the dashboard's field picker and must never widen the
// chat lane's blast radius, so both halves are pinned in one test: whatever
// makes DryRun return a body must also leave a non-DryRun reply empty.
func TestFetchDryRunReturnsSampleButChatNeverDoes(t *testing.T) {
	h := newHarness(t)
	const body = `{"forecast":{"temp":71.2}}`
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: body})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"forecast", "temp"}})

	dry := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})
	require.Equal(t, gossiprpc.FetchOK, dry.Status)
	assert.Equal(t, body, dry.Sample, "the field picker builds its tree from the real response")

	chat := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx"})
	require.Equal(t, gossiprpc.FetchOK, chat.Status)
	assert.Empty(t, chat.Sample, "upstream text must never reach the chat lane")
}

func TestFetchDryRunReturnsSampleEvenWhenPathIsWrong(t *testing.T) {
	h := newHarness(t)
	const body = `{"forecast":{"temp":71.2}}`
	h.route(t, "/wx", staged{status: http.StatusOK, ct: "application/json", body: body})
	h.addDef("wx", "/wx", gossiprpc.FetchDef{URL: "placeholder", IsActive: true, JSONPath: []string{"nope"}})

	r := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "wx", DryRun: true})
	require.Equal(t, gossiprpc.FetchBadDef, r.Status)
	assert.Equal(t, body, r.Sample, "an author whose path is wrong is exactly who needs the tree")
}

func TestSampleForGuards(t *testing.T) {
	body := []byte(`{"a":1}`)

	assert.Empty(t, sampleFor(&flight{}, body), "the dry-run flag is the only gate")
	assert.Equal(t, `{"a":1}`, sampleFor(&flight{dryRun: true}, body))
	assert.Empty(t, sampleFor(&flight{dryRun: true}, make([]byte, maxSampleBytes+1)),
		"oversized drops rather than truncates: a half body can parse as a shorter valid document whose paths do not match the real response")
	assert.Empty(t, sampleFor(&flight{dryRun: true}, []byte{0xff, 0xfe, 0x00}),
		"non-UTF-8 would not survive JSON marshalling intact")
}

func TestFetchSlowUpstreamMapsToTimeout(t *testing.T) {
	h := newHarness(t)
	h.routesMu.Lock()
	h.routes["/slow"] = staged{status: http.StatusOK, ct: "application/json", body: `{}`, delay: 3 * time.Second}
	h.routesMu.Unlock()
	h.addDef("slow", "/slow", gossiprpc.FetchDef{URL: "placeholder", IsActive: true})

	reply := call(t, h, gossiprpc.Request{ChannelID: "ch1", DefID: "slow"})
	assert.Equal(t, gossiprpc.FetchTimeout, reply.Status)
}
