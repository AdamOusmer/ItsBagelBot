// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
	"golang.org/x/net/proxy"
)

const maxBodyBytes = 4 << 20

type UpstreamError struct {
	Status     int
	Message    string
	LocalDeny  bool
	RetryAfter time.Duration
}

func (e *UpstreamError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("upstream %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("upstream status %d", e.Status)
}

var sharedTransport = newSharedTransport()

const (
	h2ReadIdleTimeout = 15 * time.Second
	h2PingTimeout     = 3 * time.Second
	idleConnTimeout   = 10 * time.Minute
)

func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dial addr %q: %w", addr, err)
	}
	ip, err := resolveAllowed(ctx, host)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

func newSharedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowPlainHTTPUpstreamsForTests.Load() {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
		return guardedDialContext(ctx, network, addr)
	}
	t.MaxIdleConns = 200
	t.MaxIdleConnsPerHost = 32
	t.IdleConnTimeout = idleConnTimeout
	t.ForceAttemptHTTP2 = true
	t.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: h2ReadIdleTimeout,
		PingTimeout:     h2PingTimeout,
	}
	return t
}

type HTTPClient struct {
	base    string
	headers map[string]string
	lane    Lane
	hc      *http.Client
}

type Lane uint8

const (
	LaneDirect Lane = iota
	LaneWARP
)

func (l Lane) String() string {
	if l == LaneWARP {
		return "warp"
	}
	return "trusted"
}

func (c *HTTPClient) Lane() Lane { return c.lane }

func ProviderClient(lane Lane, base string, headers map[string]string, timeout time.Duration) *HTTPClient {
	return newHTTPClient(lane, base, headers, timeout)
}

// Keep unexported: a direct caller skips the Builder's WARP lane choice.
func newHTTPClient(lane Lane, base string, headers map[string]string, timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HTTPClient{
		base:    base,
		headers: headers,
		lane:    lane,
		hc:      &http.Client{Timeout: timeout, Transport: transportFor(lane), CheckRedirect: redirectPolicy},
	}
}

func transportFor(lane Lane) http.RoundTripper {
	if lane == LaneWARP {
		return warpTransport
	}
	return sharedTransport
}

var warpProxyAddr = "127.0.0.1:40000"

func SetWARPProxyAddrForTests(addr string) { warpProxyAddr = addr }

func WARPProxyAddr() string { return warpProxyAddr }

func WARPReachable(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", warpProxyAddr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWARPDown, err)
	}
	_ = conn.Close()
	return nil
}

var warpTransport = newWARPTransport()

func newWARPTransport() *http.Transport {
	t := newSharedTransport()
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("warp dial addr %q: %w", addr, err)
		}
		target := addr
		// CONNECT to the pinned IP, never the hostname: a second lookup reopens DNS rebinding.
		if !allowPlainHTTPUpstreamsForTests.Load() {
			ip, rerr := resolveAllowed(ctx, host)
			if rerr != nil {
				return nil, rerr
			}
			target = net.JoinHostPort(ip.String(), port)
		}
		d, ok := currentWARPDialer().(proxy.ContextDialer)
		if !ok {
			panic("core: warp socks dialer lacks ContextDialer")
		}
		return d.DialContext(ctx, network, target)
	}
	return t
}

var (
	warpDialMu   sync.Mutex
	warpDialAddr string
	warpDial     proxy.Dialer = mustSOCKSDialer("127.0.0.1:40000")
)

func currentWARPDialer() proxy.Dialer {
	warpDialMu.Lock()
	defer warpDialMu.Unlock()
	if warpDialAddr != warpProxyAddr {
		warpDial = mustSOCKSDialer(warpProxyAddr)
		warpDialAddr = warpProxyAddr
	}
	return warpDial
}

func mustSOCKSDialer(addr string) proxy.Dialer {
	d, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
	if err != nil {
		panic("core: warp socks dialer: " + err.Error())
	}
	return d
}

// The WARP lane must fail closed on this; a direct fallback exposes the cluster IP.
var ErrWARPDown = errors.New("warp sidecar unreachable")

const maxRedirectHops = 3

var (
	ErrTooManyRedirects = errors.New("redirect limit exceeded")
	ErrHTTPSDowngrade   = errors.New("https to http redirect downgrade")
)

// Every hop must re-run the gate, or a redirect pivots to an internal host.
func redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirectHops {
		return fmt.Errorf("%w after %d hops", ErrTooManyRedirects, len(via))
	}
	if prev := via[len(via)-1].URL.Scheme; prev == "https" && req.URL.Scheme == "http" {
		return fmt.Errorf("%w to %s", ErrHTTPSDowngrade, req.URL.Host)
	}
	return SSRFCheck(req.URL)
}

func transportShape(u *url.URL) error {
	if u == nil || u.Host == "" {
		return &SSRFError{"missing host"}
	}
	if u.Scheme != "https" {
		return &SSRFError{fmt.Sprintf("scheme %q not allowed (https only)", u.Scheme)}
	}
	if p := u.Port(); p != "" && p != "443" {
		return &SSRFError{fmt.Sprintf("port %q not allowed (443 only)", p)}
	}
	return nil
}

func SSRFCheck(u *url.URL) error {
	if allowPlainHTTPUpstreamsForTests.Load() {
		return nil
	}
	if err := transportShape(u); err != nil {
		return err
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return &SSRFError{"missing host"}
	}
	// Only literal IPs are judged here; names such as "127.1" must still be judged at dial time.
	if ip, err := netip.ParseAddr(host); err == nil {
		if verr := classifyAddr(ip.Unmap()); verr != nil {
			return &SSRFError{verr.Error()}
		}
	}
	if strings.Contains(host, ":") {
		return &SSRFError{"host is not a DNS name"}
	}
	return nil
}

// Only _test.go files may set this: it turns the SSRF gate off process-wide.
var allowPlainHTTPUpstreamsForTests atomic.Bool

func SetSSRFCheckForTests(enabled bool) { allowPlainHTTPUpstreamsForTests.Store(!enabled) }

type ErrBlockedAddress struct {
	Addr netip.Addr
}

var ErrBlockedAddressPolicy = errors.New("address is not global unicast (blocked local/private/reserved space)")

func (e *ErrBlockedAddress) Error() string {
	return "address " + e.Addr.String() + ": " + ErrBlockedAddressPolicy.Error()
}

func (e *ErrBlockedAddress) Is(target error) bool { return target == ErrBlockedAddressPolicy }

var (
	blockedSpecialV4 = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	blockedSpecialV6 = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
	}
)

func classifyAddr(a netip.Addr) error {
	if !a.IsValid() {
		return fmt.Errorf("invalid address")
	}
	a = a.Unmap()
	if err := classifyLocal(a); err != nil {
		return err
	}
	switch {
	case a.Is4():
		return classifySpecial(a, blockedSpecialV4)
	case a.Is6():
		return classifyV6(a)
	}
	return fmt.Errorf("unroutable address family")
}

func classifyLocal(a netip.Addr) error {
	switch {
	case a.IsUnspecified():
		return fmt.Errorf("unspecified address")
	case a.IsLoopback():
		return fmt.Errorf("loopback address")
	case a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(), a.IsInterfaceLocalMulticast(), a.IsMulticast():
		return fmt.Errorf("link-local/multicast address")
	case a.IsPrivate():
		return fmt.Errorf("private (RFC1918/ULA) address")
	}
	return nil
}

func classifyV6(a netip.Addr) error {
	if v4, ok := embeddedV4(a); ok {
		return classifyAddr(v4)
	}
	return classifySpecial(a, blockedSpecialV6)
}

func classifySpecial(a netip.Addr, prefixes []netip.Prefix) error {
	if p, ok := matchedPrefix(a, prefixes); ok {
		return fmt.Errorf("special-purpose range %s", p)
	}
	return nil
}

// 6to4 carries the IPv4 in bits 16-48; reading the low 32 judges an attacker-chosen decoy.
func embeddedV4(a netip.Addr) (netip.Addr, bool) {
	b := a.As16()
	if netip.MustParsePrefix("2002::/16").Contains(a) {
		return v4At(b, 2)
	}
	if _, ok := matchedPrefix(a, lowBitsEmbedV4); ok {
		return v4At(b, 12)
	}
	return netip.Addr{}, false
}

var lowBitsEmbedV4 = []netip.Prefix{
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
}

func matchedPrefix(a netip.Addr, prefixes []netip.Prefix) (netip.Prefix, bool) {
	for _, p := range prefixes {
		if p.Contains(a) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

func v4At(b [16]byte, off int) (netip.Addr, bool) {
	v4, ok := netip.AddrFromSlice(b[off : off+4])
	if !ok {
		return netip.Addr{}, false
	}
	return v4.Unmap(), true
}

// Every record must pass, and callers must dial exactly the returned address (DNS rebinding).
func resolveAllowed(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(strings.TrimSuffix(host, ".")); err == nil {
		return allowedAddr(ip.Unmap())
	}
	ips, rerr := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if rerr != nil {
		return netip.Addr{}, rerr
	}
	return firstAllowed(ips, host)
}

func allowedAddr(a netip.Addr) (netip.Addr, error) {
	if verr := classifyAddr(a); verr != nil {
		return netip.Addr{}, &ErrBlockedAddress{Addr: a}
	}
	return a, nil
}

func firstAllowed(ips []netip.Addr, host string) (netip.Addr, error) {
	var first netip.Addr
	for _, ne := range ips {
		a := ne.Unmap()
		if verr := classifyAddr(a); verr != nil {
			return netip.Addr{}, &ErrBlockedAddress{Addr: a}
		}
		if !first.IsValid() {
			first = a
		}
	}
	if !first.IsValid() {
		return netip.Addr{}, fmt.Errorf("host %q resolved to no addresses", host)
	}
	return first, nil
}

type SSRFError struct{ Reason string }

func (e *SSRFError) Error() string { return "url rejected: " + e.Reason }

// Bounds the decompressed body; trusting Content-Length reopens the gzip bomb.
const customMaxBody = 1<<20 + 1

func (c *HTTPClient) FetchBounded(ctx context.Context, r Request) ([]byte, error) {
	req, err := c.newRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := SSRFCheck(req.URL); err != nil {
		return nil, err
	}

	resp, err := c.roundTrip(ctx, req, r.NoRedirects)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return vetBoundedResponse(resp)
}

func vetBoundedResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, customMaxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &UpstreamError{
			Status:     resp.StatusCode,
			Message:    upstreamMessage(body),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if ct := resp.Header.Get("Content-Type"); !allowedContentType(ct) {
		return nil, fmt.Errorf("%w: %q", ErrContentTypeNotAllowed, ct)
	}
	if len(body) > customMaxBody-1 {
		return nil, fmt.Errorf("response exceeds %d bytes: %w", customMaxBody-1, ErrBodyTooLarge)
	}
	return body, nil
}

var (
	ErrContentTypeNotAllowed = errors.New("content type not allowed")
	ErrBodyTooLarge          = errors.New("response body too large")
)

func allowedContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mt == "application/json" || strings.HasPrefix(mt, "text/")
}

type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Headers     map[string]string
	Body        []byte
	NoRedirects bool
}

func (c *HTTPClient) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: query}, out)
}

func (c *HTTPClient) Do(ctx context.Context, r Request, out any) error {
	req, err := c.newRequest(ctx, r)
	if err != nil {
		return err
	}
	if err := SSRFCheck(req.URL); err != nil {
		return err
	}

	resp, err := c.roundTrip(ctx, req, r.NoRedirects)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp, out)
}

func (c *HTTPClient) roundTrip(ctx context.Context, req *http.Request, noRedirects bool) (*http.Response, error) {
	segment := newrelic.StartExternalSegment(newrelic.FromContext(ctx), req)
	if c.lane == LaneWARP {
		segment.AddAttribute("lane", "warp")
	}
	client := c.hc
	if noRedirects {
		isolated := *client
		isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client = &isolated
	}
	resp, err := client.Do(req)
	segment.Response = resp
	segment.End()
	if err != nil {
		if c.lane == LaneWARP && errors.Is(err, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("%w: %v", ErrWARPDown, err)
		}
		return nil, err
	}
	return resp, nil
}

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

func decodeJSON(resp *http.Response, out any) error {
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &UpstreamError{
			Status:     resp.StatusCode,
			Message:    upstreamMessage(body),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if out == nil {
		return nil
	}
	return codec.Unmarshal(body, out)
}

func upstreamMessage(body []byte) string {
	var flat struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = codec.Unmarshal(body, &flat)
	if flat.Error != "" {
		return flat.Error
	}
	if flat.Message != "" {
		return flat.Message
	}
	var nested struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = codec.Unmarshal(body, &nested)
	return nested.Error.Message
}

const maxRetryAfter = 5 * time.Minute

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		return retryAfterSeconds(seconds)
	}
	t, err := http.ParseTime(raw)
	if err != nil {
		return 0
	}
	return clampRetryAfter(time.Until(t))
}

func retryAfterSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	if seconds > int(maxRetryAfter/time.Second) {
		return maxRetryAfter
	}
	return time.Duration(seconds) * time.Second
}

func clampRetryAfter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
