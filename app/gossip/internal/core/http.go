// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
)

// maxBody bounds an upstream response read. The largest legitimate payload the
// gossip service handles is a full Hypixel profile (a few hundred KB); anything past
// this is a misbehaving upstream, not data.
const maxBody = 4 << 20 // 4 MiB

// UpstreamError is a non-2xx answer from an external API, kept as a typed
// error so providers can map well-known statuses (404 -> "player not found")
// to user-facing reply errors instead of infrastructure failures.
type UpstreamError struct {
	Status int
	// Message is the upstream's own error text when it sent a JSON
	// {"error": "..."} body, empty otherwise.
	Message string
	// LocalDeny marks a 429 minted by our own token bucket rather than
	// received from the upstream. The two must stay distinguishable: a bucket
	// denial refills in seconds and cost no upstream call, while a real
	// upstream 429 means the provider is throttling our address and retrying
	// immediately makes it worse.
	LocalDeny bool
}

func (e *UpstreamError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("upstream %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("upstream status %d", e.Status)
}

// sharedTransport is the one outbound transport every provider's HTTPClient
// runs on. Pooling connections (and their TLS sessions) here — instead of each
// client falling back to http.DefaultTransport with its stingy
// 2-idle-conns-per-host default — lets repeated calls to an upstream (a burst of
// Govee control redemptions, a chat spike of stats lookups) reuse a warm
// connection instead of paying a fresh TLS handshake each time. Per-call
// timeouts still live on the individual clients.
var sharedTransport = newSharedTransport()

const (
	// h2ReadIdleTimeout is how long an HTTP/2 connection may go without
	// reading a frame before the transport pings it to prove it is alive.
	// Zero — the value the h2 transport is born with, since
	// http2setConfigDefaults assigns no default to SendPingTimeout — disarms
	// the health-check timer entirely, and the h2 transport funnels every
	// request to a host down ONE TCP connection. So a connection that dies
	// silently (a conntrack or middlebox reap, a path change across this
	// fleet's Cilium WireGuard pod plane or its VLAN mesh) is never noticed:
	// the transport keeps writing requests into a socket nobody is reading,
	// and each one hangs for the full http.Client timeout. IdleConnTimeout
	// covers only the case where the connection stays idle for its whole
	// window, not a reap inside that window and not a stall mid-flight — and
	// the wider that window grows the more of its life a connection spends
	// unproven, which is why idleConnTimeout below is written as the other
	// half of this setting. 15s is short enough that a reap is caught many
	// times over inside the idle window while costing at most one PING frame
	// per connection per 15 idle seconds; the pings do not defeat idle
	// reaping, which tracks lastIdle separately.
	h2ReadIdleTimeout = 15 * time.Second
	// h2PingTimeout is how long the health check waits for the PONG before
	// declaring the connection lost and closing it. It must fit inside a
	// request's own budget to be worth anything: the h2 default is 15s,
	// longer than the 10s NewHTTPClient timeout, so a stalled request would
	// die at its own deadline before the ping ever returned a verdict. 3s
	// leaves the verdict — and the retry onto a fresh connection — comfortably
	// inside the 10s budget, while staying far above any real RTT to an
	// upstream (single-digit ms) or between fleet nodes (sub-ms).
	h2PingTimeout = 3 * time.Second
	// idleConnTimeout is how long a pooled connection may sit unused before it
	// is closed. net/http's stock 90s is wrong for this service's shape: gossip
	// serves chat commands — a burst when viewers ask, then a long quiet
	// stretch — and that burst is split across three replicas, so any one
	// replica's gap to any one upstream runs roughly three times the channel's
	// own. Minutes between calls to api.hypixel.net or fortnite-api.com is the
	// normal case here, not the tail, which means at 90s the idle pool is
	// essentially never hit and the first request of every burst pays DNS, TCP
	// and a full TLS handshake to a remote third-party API — 100-300ms in front
	// of a command a viewer is waiting on, all day long. 10 minutes is the
	// length of a quiet stretch inside a live session; past it the channel
	// using that provider has almost certainly gone away and the socket buys
	// nothing. Overshooting costs close to nothing anyway, because the
	// effective lifetime is min(this, the upstream's own keep-alive) and a
	// server-side close arrives as a GOAWAY or FIN the read loop acts on at
	// once — a too-long value degrades into re-dialing exactly as a short one
	// would. Undershooting has no such escape: it is a handshake we chose to
	// pay.
	//
	// This is only safe because h2ReadIdleTimeout is armed, and the two are one
	// setting — lowering either alone reintroduces the original failure. While
	// SendPingTimeout was zero a long-lived idle connection was a liability: it
	// sat silent, so a conntrack or middlebox reap, or a path change across
	// this fleet's Cilium WireGuard pod plane or its VLAN mesh, blackholed it
	// invisibly, and the transport went on handing that dead socket to request
	// after request, each hanging for the full 10s client timeout — and never
	// self-clearing, since a connection with streams in flight is not idle and
	// the idle timer never fires on it. The 15s ping closes both ends of that:
	// it is real traffic on the wire, so the flow never looks idle to a reaper
	// whose window is 300s or 3600s, and when the path dies anyway the missing
	// PONG closes the connection within h2ReadIdleTimeout+h2PingTimeout instead
	// of never. The pings do not extend this window in exchange — only
	// forgetStreamID moves lastIdle, so frame reads keep the wire warm without
	// touching our own deadline.
	idleConnTimeout = 10 * time.Minute
)

func newSharedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// These two size the HTTP/1 idle pool and nothing else. The bundled h2
	// transport keeps its connections in its own clientConnPool, which reads
	// neither field, and h2 multiplexes every request to a host onto a single
	// connection regardless — so against upstreams that negotiate h2 (all of
	// today's, being TLS APIs behind modern edges) both numbers are inert.
	// They stay as the correct sizing for any upstream that falls back to
	// HTTP/1.1, where the alternative is the stock 2-idle-conns-per-host that
	// would hand a burst of stats lookups a fresh handshake apiece.
	t.MaxIdleConns = 200
	t.MaxIdleConnsPerHost = 32
	// IdleConnTimeout does reach the h2 connections: configureTransports leaves
	// http2Transport.IdleConnTimeout zero and keeps a back-pointer to us, and
	// http2Transport.idleConnTimeout falls through to t1's value, which then
	// arms each h2 ClientConn's idle timer and backs its tooIdleLocked check.
	t.IdleConnTimeout = idleConnTimeout
	t.ForceAttemptHTTP2 = true
	// net/http's own HTTP2Config, not golang.org/x/net/http2.ConfigureTransports:
	// ForceAttemptHTTP2 makes the bundled h2 transport the owner of this
	// transport's TLSNextProto, and configureTransports keeps a back-pointer
	// to us (t1), so configFromTransport merges these fields in via
	// fillNetHTTPConfig on every connection it builds. Reaching for x/net
	// instead would install a second, competing h2 implementation and
	// promote an indirect dependency to a direct one for no gain.
	t.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: h2ReadIdleTimeout,
		PingTimeout:     h2PingTimeout,
	}
	return t
}

// HTTPClient is the outbound fetcher a provider dials its API with: one base
// URL, a fixed header set (API keys), and a bounded per-request timeout. All
// clients share sharedTransport, so connection reuse spans providers.
type HTTPClient struct {
	base    string
	headers map[string]string
	hc      *http.Client
}

// NewHTTPClient builds a fetcher for base (scheme + host, no trailing slash).
// headers are attached to every request; timeout bounds each call.
func NewHTTPClient(base string, headers map[string]string, timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPClient{base: base, headers: headers, hc: &http.Client{Timeout: timeout, Transport: sharedTransport}}
}

// Request is one outbound call: the HTTP method, a path appended to the base
// URL, optional query, per-request headers (merged over the client's fixed set)
// and an optional JSON body. Bundling these keeps the call surface to one
// argument for callers whose credential is per-request rather than per-service
// (govee, where each broadcaster brings their own API key).
type Request struct {
	Method  string
	Path    string
	Query   url.Values
	Headers map[string]string
	Body    []byte
}

// GetJSON fetches base+path?query and decodes the JSON body into out. A non-2xx
// status returns an *UpstreamError carrying the upstream's own error message
// when it sent one.
func (c *HTTPClient) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: query}, out)
}

// Do performs one request/response cycle and decodes the JSON reply into out.
func (c *HTTPClient) Do(ctx context.Context, r Request, out any) error {
	req, err := c.newRequest(ctx, r)
	if err != nil {
		return err
	}

	// Report the call to New Relic as an external segment. Without this a
	// handler's transaction is one opaque block, so "the provider is slow" and
	// "we are slow" are indistinguishable in the only place that has the whole
	// picture — which is exactly the question a slow !fnstats raises, and one
	// that cost a manual round of curl-from-a-probe-pod to answer once. The
	// segment ends before the body is read, so it measures the upstream's time
	// to first byte and not our decode; decodeJSON's read and unmarshal stay in
	// the surrounding transaction, which is where they belong.
	segment := newrelic.StartExternalSegment(newrelic.FromContext(ctx), req)
	resp, err := c.hc.Do(req)
	segment.Response = resp
	segment.End()
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp, out)
}

// newRequest builds the *http.Request: URL from base+path+query, the standard
// headers, the client's fixed headers, then the per-request headers.
func (c *HTTPClient) newRequest(ctx context.Context, r Request) (*http.Request, error) {
	u := c.base + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	var body io.Reader
	if r.Body != nil {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ItsBagelBot-gossip/1.0")
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// decodeJSON reads a bounded body, maps a non-2xx to an *UpstreamError
// (carrying the upstream's own error text when present), and otherwise decodes
// the body into out.
func decodeJSON(resp *http.Response, out any) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &UpstreamError{Status: resp.StatusCode, Message: upstreamMessage(body)}
	}
	return codec.Unmarshal(body, out)
}

// upstreamMessage pulls the upstream's own error text from a JSON error body,
// tolerating either the fleet's "error" field or Govee's "message" field.
func upstreamMessage(body []byte) string {
	var envelope struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = codec.Unmarshal(body, &envelope)
	if envelope.Error != "" {
		return envelope.Error
	}
	return envelope.Message
}
