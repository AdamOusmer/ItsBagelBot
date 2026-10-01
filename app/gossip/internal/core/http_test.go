// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedTransportArmsH2HealthCheck(t *testing.T) {
	tr := newSharedTransport()

	assert.True(t, tr.ForceAttemptHTTP2)

	require.NotNil(t, tr.HTTP2, "h2 config must be set or the transport never health-checks")
	assert.Equal(t, h2ReadIdleTimeout, tr.HTTP2.SendPingTimeout)
	assert.Equal(t, h2PingTimeout, tr.HTTP2.PingTimeout)
	assert.NotZero(t, tr.HTTP2.SendPingTimeout, "a zero SendPingTimeout disarms the health-check timer")
}

func TestSharedTransportH2TimeoutsFitBudgets(t *testing.T) {
	tr := newSharedTransport()
	require.NotNil(t, tr.HTTP2)

	const defaultClientTimeout = 10 * time.Second
	assert.Less(t, tr.HTTP2.PingTimeout, defaultClientTimeout,
		"the PONG verdict must arrive before the request's own deadline, or it is useless")
	assert.Less(t, tr.HTTP2.SendPingTimeout, tr.IdleConnTimeout,
		"a connection reaped inside the idle window is exactly what the ping is for")
}

func TestSharedTransportIdleWindowIsCoveredByHealthCheck(t *testing.T) {
	tr := newSharedTransport()
	require.NotNil(t, tr.HTTP2)
	require.NotZero(t, tr.HTTP2.SendPingTimeout,
		"an idle window this long with the health check disarmed is exactly the old bug")

	const minLivenessProofs = 8
	proofs := int(tr.IdleConnTimeout / tr.HTTP2.SendPingTimeout)
	assert.GreaterOrEqual(t, proofs, minLivenessProofs,
		"a connection held this long must be pinged far more often than it is reaped")

	assert.Less(t, tr.HTTP2.SendPingTimeout+tr.HTTP2.PingTimeout, tr.IdleConnTimeout,
		"the full detect-and-close cycle must fit inside the idle window")
}

func TestSharedTransportIdleWindowOutlastsBurstGaps(t *testing.T) {
	stock := http.DefaultTransport.(*http.Transport).IdleConnTimeout
	require.Equal(t, 90*time.Second, stock,
		"net/http's default is the baseline this setting is a deliberate departure from")

	tr := newSharedTransport()
	assert.Equal(t, idleConnTimeout, tr.IdleConnTimeout)
	assert.Greater(t, tr.IdleConnTimeout, stock,
		"inheriting the stock window means paying a handshake per burst")
	assert.GreaterOrEqual(t, tr.IdleConnTimeout, 5*time.Minute,
		"per-replica gaps to one upstream run minutes once three replicas split the load")
}

func TestSharedTransportSizesTheHTTP1IdlePool(t *testing.T) {
	tr := newSharedTransport()
	assert.Greater(t, tr.MaxIdleConnsPerHost, http.DefaultMaxIdleConnsPerHost,
		"the stingy 2-per-host default is what pooling here exists to escape")
	assert.GreaterOrEqual(t, tr.MaxIdleConns, tr.MaxIdleConnsPerHost,
		"a global cap below the per-host cap would make the per-host one unreachable")
}

func TestSharedTransportOwnsItsH2Config(t *testing.T) {
	stock := http.DefaultTransport.(*http.Transport)
	a, b := newSharedTransport(), newSharedTransport()

	assert.NotSame(t, a.HTTP2, b.HTTP2)
	assert.NotSame(t, stock.HTTP2, a.HTTP2, "sharing DefaultTransport's h2 config leaks our timeouts process-wide")
	assert.Zero(t, h2ConfigOf(stock), "DefaultTransport's h2 config must stay stock")
}

func h2ConfigOf(tr *http.Transport) http.HTTP2Config {
	if tr.HTTP2 == nil {
		return http.HTTP2Config{}
	}
	return *tr.HTTP2
}

func TestNewHTTPClientUsesSharedTransport(t *testing.T) {
	c := newHTTPClient(LaneDirect, "https://example.invalid", nil, 0)
	assert.Same(t, sharedTransport, c.hc.Transport)
	assert.Equal(t, 10*time.Second, c.hc.Timeout, "a non-positive timeout falls back to 10s")
}

func TestDecodeJSONNilOutSucceedsOnAny2xx(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"204 empty, the documented shape", http.StatusNoContent, ""},
		{"200 with an empty body", http.StatusOK, ""},
		{"200 with a body Spotify was observed sending", http.StatusOK, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}
			assert.NoError(t, decodeJSON(resp, nil))
		})
	}
}

func TestDecodeJSONNilOutStillReportsUpstreamError(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"error":"not found"}`))}
	err := decodeJSON(resp, nil)
	require.Error(t, err)
	var ue *UpstreamError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, http.StatusNotFound, ue.Status)
}

func TestUpstreamMessageReadsFlatAndNestedShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"fleet flat error field", `{"error":"player not found"}`, "player not found"},
		{"govee flat message field", `{"message":"invalid device"}`, "invalid device"},
		{"spotify nested error.message", `{"error":{"status":403,"message":"Insufficient client scope"}}`, "Insufficient client scope"},
		{"unrecognized shape yields empty, not a decode error", `{"status":403}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, upstreamMessage([]byte(tc.body)))
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	assert.Equal(t, time.Duration(0), parseRetryAfter("-5"))
	assert.Equal(t, 120*time.Second, parseRetryAfter("120"))
	assert.Equal(t, 30*time.Second, parseRetryAfter("  30  "))
	assert.Equal(t, time.Duration(0), parseRetryAfter("not-a-delay"))

	future := time.Now().Add(45 * time.Second).UTC()
	parsed := parseRetryAfter(future.Format(http.TimeFormat))
	assert.InDelta(t, 45*time.Second, parsed, float64(2*time.Second))

	past := time.Now().Add(-45 * time.Second).UTC()
	assert.Equal(t, time.Duration(0), parseRetryAfter(past.Format(http.TimeFormat)))
}

func TestParseRetryAfterIsBounded(t *testing.T) {
	assert.Equal(t, maxRetryAfter, parseRetryAfter("86400"), "a well-formed day is capped, not obeyed")
	assert.Equal(t, maxRetryAfter, parseRetryAfter("10000000000"), "the value that used to wrap to -2346317h")
	assert.Equal(t, maxRetryAfter, parseRetryAfter("999999999999"))
	assert.Positive(t, parseRetryAfter("10000000000"), "an overflowed delay must never read as negative")

	farFuture := time.Now().Add(72 * time.Hour).UTC()
	assert.Equal(t, maxRetryAfter, parseRetryAfter(farFuture.Format(http.TimeFormat)), "the HTTP-date form is capped too")
}

func TestDecodeJSONExtractsRetryAfter(t *testing.T) {
	header := make(http.Header)
	header.Set("Retry-After", "90")
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"status":429,"message":"API rate limit exceeded"}}`)),
	}
	err := decodeJSON(resp, nil)
	require.Error(t, err)
	var ue *UpstreamError
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, http.StatusTooManyRequests, ue.Status)
	assert.Equal(t, 90*time.Second, ue.RetryAfter)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestSSRFCheckVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		ok   bool
	}{
		{"plain https", "https://api.example.com/v1", true},
		{"https default port implicit", "https://api.example.com:443/x", true},
		{"trailing fqdn dot kept legal", "https://api.example.com./x", true},
		{"a name containing localhost is judged as a name", "https://notlocalhost.example/x", true},
		{"http refused at fetch too", "http://api.example.com/", false},
		{"ftp scheme", "ftp://api.example.com/", false},
		{"odd port", "https://api.example.com:8443/x", false},
		{"internal-suffix name defers to dial-time truth", "https://vault.internal/x", true},
		{"svc name defers to dial-time truth", "https://valkey.cache.svc.cluster.local/x", true},
		{"bare single-label name defers to dial-time truth", "https://valkey/x", true},
		{"inet_aton shorthand is a domain now", "https://127.1/x", true},
		{"decimal shorthand is a domain now", "https://2130706433/x", true},
		{"missing host", "https:///path", false},
		{"ipv6 zone host is not a dns name", "https://[fe80::1%25eth0]/x", false},
		{"public ipv4 literal", "https://93.184.216.34/x", true},
		{"loopback", "https://127.0.0.1/x", false},
		{"ipv6 loopback", "https://[::1]/x", false},
		{"unspecified ipv4", "https://0.0.0.0/x", false},
		{"unspecified ipv6", "https://[::]/x", false},
		{"rfc1918 10/8", "https://10.0.0.5/x", false},
		{"rfc1918 172.16/12", "https://172.16.31.9/x", false},
		{"rfc1918 192.168/16", "https://192.168.1.1/x", false},
		{"link-local metadata address", "https://169.254.169.254/latest/meta-data/", false},
		{"ipv6 link-local", "https://[fe80::1]/x", false},
		{"ipv6 unique local fc00", "https://[fc00::5]/x", false},
		{"ipv6 unique local fd00", "https://[fd12:3456::1]/x", false},
		{"ipv4 multicast", "https://224.0.0.1/x", false},
		{"ipv6 link-local multicast", "https://[ff02::1]/x", false},
		{"ipv6 interface-local multicast", "https://[ff01::2]/x", false},
		{"cgnat", "https://100.64.7.9/x", false},
		{"benchmarking range", "https://198.18.5.5/x", false},
		{"test-net-1", "https://192.0.2.77/x", false},
		{"test-net-2", "https://198.51.100.10/x", false},
		{"test-net-3", "https://203.0.113.99/x", false},
		{"reserved 240/4", "https://240.1.2.3/x", false},
		{"limited broadcast", "https://255.255.255.255/x", false},
		{"ipv6 discard prefix", "https://[100::1]/x", false},
		{"ipv6 documentation prefix", "https://[2001:db8::20]/x", false},
		{"ipv6 teredo prefix", "https://[2001::4240]/x", false},
		{"mapped v4 loopback", "https://[::ffff:127.0.0.1]/x", false},
		{"mapped v4 rfc1918", "https://[::ffff:10.0.0.9]/x", false},
		{"nat64 wrapped loopback", "https://[64:ff9b::7f00:1]/x", false},
		{"nat64 wrapped public decoy in the low bits", "https://[64:ff9b::a2b:1]/x", false},
		{"6to4 wrapped rfc1918", "https://[2002:a00:1::]/x", false},
		{"6to4 wrapped rfc1918 with a public decoy in the low bits", "https://[2002:a00:1::808:808]/x", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := SSRFCheck(mustURL(t, tc.url))
			if tc.ok {
				assert.NoError(t, err)
				return
			}
			var se *SSRFError
			assert.ErrorAs(t, err, &se, "rejections must stay typed")
		})
	}
}

func TestHostnamesThatResolveToLocalSpaceAreRefusedAtDial(t *testing.T) {
	client := ProviderClient(LaneDirect, "https://localhost", nil, 2*time.Second)

	err := client.GetJSON(context.Background(), "/x", nil, &struct{}{})

	require.ErrorIs(t, err, ErrBlockedAddressPolicy)
}

func TestRedirectChainsAreCappedAtThreeHops(t *testing.T) {
	SetSSRFCheckForTests(false)
	t.Cleanup(func() { SetSSRFCheckForTests(true) })
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	err := ProviderClient(LaneDirect, srv.URL, nil, 2*time.Second).GetJSON(context.Background(), "/start", nil, &struct{}{})

	require.ErrorIs(t, err, ErrTooManyRedirects)
	assert.Equal(t, maxRedirectHops, hits, "the third redirect response is refused before it is followed")
}

func TestWARPLaneFailsClosedWhenSidecarDown(t *testing.T) {
	prev := WARPProxyAddr()
	SetWARPProxyAddrForTests("127.0.0.1:1")
	t.Cleanup(func() { SetWARPProxyAddrForTests(prev) })

	c := ProviderClient(LaneWARP, "https://93.184.216.34", nil, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.FetchBounded(ctx, Request{Method: http.MethodGet, Path: "/x"})
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the budget")
}

func TestWARPReachable(t *testing.T) {
	prev := WARPProxyAddr()
	t.Cleanup(func() { SetWARPProxyAddrForTests(prev) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	SetWARPProxyAddrForTests(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, WARPReachable(ctx), "a bound listener is reachable")

	SetWARPProxyAddrForTests("127.0.0.1:1")
	err = WARPReachable(ctx)
	require.ErrorIs(t, err, ErrWARPDown)
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"a refused listener must fail fast, not burn the probe budget")
}
