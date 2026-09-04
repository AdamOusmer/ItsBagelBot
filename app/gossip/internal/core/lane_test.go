// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The gate is the one check standing between a broadcaster-authored URL and a
// dial. Its string half pins transport shape (scheme, port, host presence)
// and literal-IP classification; NAMES are deliberately not judged here —
// every name, however spelled, ends at resolveAllowed/classifyAddr where the
// single address invariant lives.
func TestSSRFCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		ok   bool
	}{
		{"plain https", "https://api.example.com/v1", true},
		{"https default port implicit", "https://api.example.com:443/x", true},
		{"trailing fqdn dot kept legal", "https://api.example.com./x", true},
		{"http refused at fetch too", "http://api.example.com/", false},
		{"ftp scheme", "ftp://api.example.com/", false},
		{"odd port", "https://api.example.com:8443/x", false},
		{"ipv4 literal", "https://127.0.0.1/x", false},
		{"metadata ipv4 literal", "https://169.254.169.254/latest/meta-data/", false},
		{"rfc1918 literal", "https://10.0.0.5/x", false},
		{"cgnat literal", "https://100.64.1.1/x", false},
		{"benchmark literal", "https://198.18.0.0/x", false},
		{"test-net literal", "https://192.0.2.9/x", false},
		{"nat64 wrapped loopback", "https://[64:ff9b::7f00:1]/x", false},
		{"6to4 wrapped rfc1918", "https://[2002:a00:1::]/x", false},
		{"mapped v4 loopback", "https://[::ffff:127.0.0.1]/x", false},
		{"ipv6 loopback bracketed", "https://[::1]/x", false},
		{"ipv6 linklocal bracketed", "https://[fe80::1]/x", false},
		{"ipv6 any", "https://[::]/x", false},
		{"ipv6 zone host is not a dns name", "https://[fe80::1%25eth0]/x", false},
		// Names pass the string gate by design — their ADDRESSES are judged at
		// resolution/dial time, so no naming convention can outrun the rule:
		{"internal-suffix name defers to dial-time truth", "https://vault.internal/x", true},
		{"svc name defers to dial-time truth", "https://valkey.cache.svc.cluster.local/x", true},
		{"bare single-label name defers to dial-time truth", "https://valkey/x", true},
		{"inet_aton shorthand is a domain now", "https://127.1/x", true},
		{"decimal shorthand is a domain now", "https://2130706433/x", true},
		{"missing host", "https:///path", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := SSRFCheck(mustURL(t, tc.url))
			if tc.ok {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				var se *SSRFError
				assert.ErrorAs(t, err, &se, "rejections must stay typed")
			}
		})
	}
}

// mustURL parses a fixture or fails the test; every row above is static and
// well-formed.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func mustRequest(t *testing.T, u *url.URL) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	require.NoError(t, err)
	return req
}

// The denylist matches whole labels only in spirit but is implemented as
// suffix match after lowering; this pins the one dangerous overlap — a legit
// host that merely CONTAINS a denied word ("notlocalhost.example") stays
// dialable while every real denial shape still trips.
// The suffix-list era is over: names are no longer string-matched anywhere,
// so the old "denylist is whole-label" property moved to classification —
// pinned here in its new home. A name that merely CONTAINS a local word but
// resolves public stays dialable; the address decides.
func TestNameRulesAreGoneClassificationDecides(t *testing.T) {
	require.NoError(t, SSRFCheck(mustURL(t, "https://notlocalhost.example/x")))
}

// classifyAddr is the entire gate reduced to one predicate: allow GLOBAL
// UNICAST, refuse everything else. The table pins the blocked space (loopback,
// unspecified, RFC1918, ULA, v4/v6 link-local incl. cloud metadata, multicast
// flavors, CGNAT, benchmarking, TEST-NETs, reserved/limited-broadcast,
// discard-only, Teredo, documentation) AND the translation wrappers a hostile
// author hides behind — NAT64, 6to4, IPv4-mapped — whose embedded IPv4 must
// be re-judged, not taken at face value.
func TestClassifyAddrAllowsOnlyGlobalUnicast(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool // true = dialable
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"10.0.0.5", false},
		{"172.16.31.9", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"fe80::1", false},
		{"fc00::5", false}, // ULA
		{"fd12:3456::1", false},
		{"224.0.0.1", false},        // v4 multicast
		{"ff02::1", false},          // link-local multicast
		{"ff01::2", false},          // interface-local multicast
		{"100.64.7.9", false},       // CGNAT
		{"198.18.5.5", false},       // benchmarking
		{"192.0.2.77", false},       // TEST-NET-1
		{"198.51.100.10", false},    // TEST-NET-2
		{"203.0.113.99", false},     // TEST-NET-3
		{"240.1.2.3", false},        // reserved
		{"255.255.255.255", false},  // limited broadcast
		{"100::1", false},           // discard-only
		{"2001:db8::20", false},     // documentation
		{"2001::4240", false},       // Teredo
		{"::ffff:127.0.0.1", false}, // mapped loopback
		{"::ffff:10.0.0.9", false},  // mapped RFC1918
		{"64:ff9b::7f00:1", false},  // NAT64 wrapping 127.0.0.1
		{"2002:a00:1::", false},     // 6to4 wrapping 10.0.0.1
		// 6to4 embeds its IPv4 in bits 16-48 (RFC 3056), NOT the low 32: the
		// decoy low bytes here read 8.8.8.8 while the wrapped address is
		// 10.0.0.1 — the shape the wrong-byte extraction bug allowed through.
		{"2002:a00:1::808:808", false},
		{"2002:5db8:d822::", true}, // 6to4 wrapping public 93.184.216.34 dials
		{"64:ff9b::a2b:1", false},  // NAT64 wrapping 10.43.0.1 (k3s svc CIDR)
		{"93.184.216.34", true},    // public unicast dials
		{"2606:2800:220:1:248:1893:25c8:1946", true},
	} {
		err := classifyAddr(netip.MustParseAddr(tc.ip))
		if tc.want {
			assert.NoError(t, err, "%s must dial", tc.ip)
		} else {
			assert.Error(t, err, "%s must be refused", tc.ip)
		}
	}
}

// The guard holds under the one attack shape no name rule can see: rebinding.
// "localhost" resolves to loopback on every platform, so its NAME passes any
// string heuristic while guardedDialContext must still refuse it — and pin
// the refusal as policy (ErrBlockedAddress), not transport noise.
func TestGuardedDialRefusesLoopbackResolution(t *testing.T) {
	if allowPlainHTTPUpstreamsForTests.Load() {
		t.Skip("gate disabled by another test's escape hatch")
	}
	// "localhost" always resolves to loopback on every platform; the name
	// rules would have caught it too, so use a form they cannot: an IP is
	// checked directly, proving the literal path also validates.
	_, err := guardedDialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "443"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocked")
}

func TestRedirectPolicy(t *testing.T) {
	start := mustURL(t, "https://api.example.com/a")

	t.Run("caps the chain at three hops", func(t *testing.T) {
		via := make([]*http.Request, maxRedirectHops)
		for i := range via {
			via[i] = mustRequest(t, start)
		}
		err := redirectPolicy(mustRequest(t, mustURL(t, "https://api.example.com/b")), via)
		require.ErrorIs(t, err, ErrTooManyRedirects)
	})

	t.Run("forbids https to http downgrade as a typed error", func(t *testing.T) {
		req := mustRequest(t, mustURL(t, "http://api.example.com/b"))
		err := redirectPolicy(req, []*http.Request{mustRequest(t, start)})
		require.ErrorIs(t, err, ErrHTTPSDowngrade)
	})

	t.Run("re-runs the full gate on the hop target", func(t *testing.T) {
		req := mustRequest(t, mustURL(t, "https://127.0.0.1/b"))
		err := redirectPolicy(req, []*http.Request{mustRequest(t, start)})
		var se *SSRFError
		require.ErrorAs(t, err, &se, "a redirect to an IP literal dies in the gate, not the hop cap")
	})

	t.Run("same-host https hop passes", func(t *testing.T) {
		req := mustRequest(t, mustURL(t, "https://api.example.com/b"))
		assert.NoError(t, redirectPolicy(req, []*http.Request{mustRequest(t, start)}))
	})
}

// Lane wiring: direct rides the shared transport, warp rides its own SOCKS
// transport, and ProviderClient is the single exported constructor both go
// through.
func TestLaneTransportSelection(t *testing.T) {
	direct := newHTTPClient(LaneDirect, "https://a.invalid", nil, 0)
	warp := newHTTPClient(LaneWARP, "https://a.invalid", nil, 0)
	assert.Same(t, sharedTransport, direct.hc.Transport)
	assert.Same(t, warpTransport, warp.hc.Transport)
	assert.NotSame(t, direct.hc.Transport, warp.hc.Transport,
		"a lane sharing transports would let untrusted traffic ride direct egress")
	assert.NotNil(t, warp.hc.Transport.(*http.Transport).DialContext,
		"the warp lane must own its dialing or it would not proxy at all")
}

// The WARP transport carries over everything the shared transport was tuned
// for: the h2 health-check pair (a dead tunnel is indistinguishable from a
// dead direct connection), the idle window sized for chat-burst gaps, the h1
// fallback pool sizing, and ForceAttemptHTTP2 (TLS terminates at the origin
// through the tunnel, so ALPN still negotiates h2 despite the custom dialer).
func TestWARPTransportCarriesSharedTuning(t *testing.T) {
	tr := newWARPTransport()

	assert.True(t, tr.ForceAttemptHTTP2,
		"without ForceAttemptHTTP2 a custom DialContext disables h2 entirely")
	require.NotNil(t, tr.HTTP2)
	assert.Equal(t, h2ReadIdleTimeout, tr.HTTP2.SendPingTimeout)
	assert.Equal(t, h2PingTimeout, tr.HTTP2.PingTimeout)
	assert.Equal(t, idleConnTimeout, tr.IdleConnTimeout)
	assert.Equal(t, sharedTransport.MaxIdleConns, tr.MaxIdleConns)
	assert.Equal(t, sharedTransport.MaxIdleConnsPerHost, tr.MaxIdleConnsPerHost)
}

// Fail-closed: with nothing listening on the sidecar's loopback port, a WARP
// request surfaces typed ErrWARPDown and NEVER falls back to a direct dial.
// The target is a PUBLIC IP literal, so classification passes and the dial
// reaches the dead listener — proving the failure is the TUNNEL, not the
// gate, and that nothing falls back to direct egress either way.
func TestWARPLaneFailsClosedWhenSidecarDown(t *testing.T) {
	prev := warpProxyAddr
	warpProxyAddr = "127.0.0.1:1" // reserved port: connection refused, instantly
	t.Cleanup(func() { warpProxyAddr = prev })

	c := newHTTPClient(LaneWARP, "https://93.184.216.34", nil, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.FetchBounded(ctx, Request{Method: http.MethodGet, Path: "/x"})
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the budget")
}

// The health check reads the same listener the WARP lane dials, and reports
// the same typed error, so /status and a failing fetch name one cause. A bound
// listener passing is the whole positive claim — the check deliberately proves
// nothing about the tunnel behind it.
func TestWARPReachable(t *testing.T) {
	prev := warpProxyAddr
	t.Cleanup(func() { warpProxyAddr = prev })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	warpProxyAddr = ln.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, WARPReachable(ctx), "a bound listener is reachable")

	warpProxyAddr = "127.0.0.1:1" // reserved port: connection refused, instantly
	err = WARPReachable(ctx)
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the probe budget")
}
