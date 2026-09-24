// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"io"
	"net/http"
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
	require.Nil(t, http.DefaultTransport.(*http.Transport).HTTP2,
		"DefaultTransport carries no h2 config; ours is the only source")

	a, b := newSharedTransport(), newSharedTransport()
	require.NotNil(t, a.HTTP2)
	require.NotNil(t, b.HTTP2)
	assert.NotSame(t, a.HTTP2, b.HTTP2)
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
