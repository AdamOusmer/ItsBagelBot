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

func TestNameRulesAreGoneClassificationDecides(t *testing.T) {
	require.NoError(t, SSRFCheck(mustURL(t, "https://notlocalhost.example/x")))
}

func TestClassifyAddrAllowsOnlyGlobalUnicast(t *testing.T) {
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"10.0.0.5", false},
		{"172.16.31.9", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false},
		{"fe80::1", false},
		{"fc00::5", false},
		{"fd12:3456::1", false},
		{"224.0.0.1", false},
		{"ff02::1", false},
		{"ff01::2", false},
		{"100.64.7.9", false},
		{"198.18.5.5", false},
		{"192.0.2.77", false},
		{"198.51.100.10", false},
		{"203.0.113.99", false},
		{"240.1.2.3", false},
		{"255.255.255.255", false},
		{"100::1", false},
		{"2001:db8::20", false},
		{"2001::4240", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.9", false},
		{"64:ff9b::7f00:1", false},
		{"2002:a00:1::", false},
		{"2002:a00:1::808:808", false},
		{"2002:5db8:d822::", true},
		{"64:ff9b::a2b:1", false},
		{"93.184.216.34", true},
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

func TestGuardedDialRefusesLoopbackResolution(t *testing.T) {
	if allowPlainHTTPUpstreamsForTests.Load() {
		t.Skip("gate disabled by another test's escape hatch")
	}
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

func TestWARPLaneFailsClosedWhenSidecarDown(t *testing.T) {
	prev := warpProxyAddr
	warpProxyAddr = "127.0.0.1:1"
	t.Cleanup(func() { warpProxyAddr = prev })

	c := newHTTPClient(LaneWARP, "https://93.184.216.34", nil, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.FetchBounded(ctx, Request{Method: http.MethodGet, Path: "/x"})
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the budget")
}

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

	warpProxyAddr = "127.0.0.1:1"
	err = WARPReachable(ctx)
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the probe budget")
}
