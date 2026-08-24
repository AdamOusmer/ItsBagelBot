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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
	"golang.org/x/net/proxy"
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
	// longer than the 10s default client timeout, so a stalled request would
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

// guardedDialContext dials only a vetted address: resolve (or take the
// literal), classify, then connect to the PINNED IP. TLS ServerName still
// derives from the request URL, so certificate identity and SNI are
// unaffected by dialing the literal IP.
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
	// Dial-time guard (see guardedDialContext): the trusted lane never dials
	// an unvetted address. The test escape is checked per dial, not per
	// construction, because sibling packages flip it from their init() after
	// this transport already exists.
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowPlainHTTPUpstreamsForTests.Load() {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
		return guardedDialContext(ctx, network, addr)
	}
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
// URL, a fixed header set (API keys), a bounded per-request timeout, and the
// egress lane it dials through. Clients on LaneDirect share sharedTransport;
// clients on LaneWARP share warpTransport — so connection reuse spans the
// providers of each lane.
type HTTPClient struct {
	base    string
	headers map[string]string
	lane    Lane
	hc      *http.Client
}

// Lane selects the egress path an HTTPClient dials through. The default for
// anything built outside this package is decided by provider.Builder: user
// defined fetches egress via WARP unless a provider explicitly declared
// itself trusted.
type Lane uint8

const (
	// LaneDirect is the trusted lane: today's shared transport dialing straight
	// from the pod IP. Only config-owned hosts (the nine stats providers) may
	// ride it; provider.Builder hands it out only after an explicit .Trusted().
	LaneDirect Lane = iota
	// LaneWARP routes every dial through the Cloudflare WARP sidecar's SOCKS
	// listener on loopback, so target hosts never see a cluster IP and cannot
	// null-route one. DNS resolves LOCALLY through the same guarded path as
	// the direct lane and the SOCKS CONNECT carries the PINNED IP (see
	// newWARPTransport) — classification is ours on both lanes, only the
	// packet path differs; any edge-side Gateway policy is defense in depth,
	// never the gate. A dead sidecar fails closed as ErrWARPDown — never a
	// direct fallback.
	LaneWARP
)

func (l Lane) String() string {
	if l == LaneWARP {
		return "warp"
	}
	return "trusted"
}

// Lane reports the egress lane this client dials on — the read half of the
// Builder chokepoint, so tests and boot assertions can pin who dials where.
func (c *HTTPClient) Lane() Lane { return c.lane }

// ProviderClient is THE one exported route to an outbound client, and it takes
// the lane explicitly. It exists for provider.Builder, which decides the lane
// from the provider's trust declaration, records every construction in
// []clientSpec, and logs the tally at boot. Anything else calling it directly
// is exactly the bypass the Builder chokepoint was built to make visible: a
// new call site here is a reviewable diff naming its lane in review, not a
// silent constructor inside some handler closure.
func ProviderClient(lane Lane, base string, headers map[string]string, timeout time.Duration) *HTTPClient {
	return newHTTPClient(lane, base, headers, timeout)
}

// newHTTPClient builds a fetcher for base (scheme + host, no trailing slash).
// headers are attached to every request; timeout bounds each call. Unexported
// on purpose: since the rename from NewHTTPClient, the only way to reach any
// outbound client from outside core is ProviderClient above — adding another
// export to this file is the deliberate, reviewed act.
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

// warpProxyAddr is the WARP sidecar's SOCKS5 listener: bound to in-pod
// loopback by `warp-cli mode proxy` before enrollment completes, so no other
// workload can ever ride it (deploy/k8s/gossip.yaml). A var solely so tests
// can point it at their own listener; production always runs 40000.
var warpProxyAddr = "127.0.0.1:40000"

// SetWARPProxyAddrForTests repoints the SOCKS listener; tests stage their own
// forwarder so the WARP lane is exercised end-to-end. Production never calls
// this — the address is fixed by the sidecar manifest (containerPort 40000).
func SetWARPProxyAddrForTests(addr string) { warpProxyAddr = addr }

// WARPProxyAddr reports the listener address SetWARPProxyAddrForTests may
// have repointed; tests use it to restore.
func WARPProxyAddr() string { return warpProxyAddr }

// warpTransport is the untrusted lane's transport: same h2 health-check pair,
// idle window and pool sizing as sharedTransport (a dead tunnel is
// indistinguishable from a dead direct connection, and the pairing argument in
// those constants stands unchanged), with DialContext swapped for a
// resolve-classify-then-SOCKS5-CONNECT-to-the-pinned-IP dialer. DNS resolves
// LOCALLY through the same guarded path as the direct lane — one verdict, one
// lookup, zero rebinding window — and only the PACKET PATH differs: the
// pinned address rides the WARP tunnel, so egress stays Cloudflare's while
// classification stays ours. net/http keys proxied connections by (proxy,
// target host), so tunnels still pool per-origin through the single localhost
// proxy.
//
// ForceAttemptHTTP2 stays set even though we now own DialContext: TLS still
// terminates at the ORIGIN through the tunnel (WARP is L3/IP-layer, not a TLS
// terminator), so ALPN negotiates h2 normally and both h2 ping constants keep
// guarding the tunnel the way they guard direct connections. SNI likewise
// derives from the request URL, not the CONNECT target, so origin certificates
// keep validating against the hostname the author typed.
var warpTransport = newWARPTransport()

func newWARPTransport() *http.Transport {
	t := newSharedTransport()
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("warp dial addr %q: %w", addr, err)
		}
		target := addr
		// Production: resolve and classify LOCALLY, then CONNECT to the
		// pinned IP. The verdict and the connect therefore reference one
		// lookup — no rebinding window — and every address is judged by
		// classifyAddr exactly like the direct lane. Only the PATH differs:
		// packets ride the WARP tunnel, so egress stays Cloudflare's.
		// Under the test escape (plain-http fake upstreams on loopback) the
		// hostname passes through untouched so staged forwarders behave as
		// before; production never sets that flag.
		if !allowPlainHTTPUpstreamsForTests.Load() {
			ip, rerr := resolveAllowed(ctx, host)
			if rerr != nil {
				return nil, rerr
			}
			target = net.JoinHostPort(ip.String(), port)
		}
		d, ok := currentWARPDialer().(proxy.ContextDialer)
		if !ok {
			panic("core: warp socks dialer lacks ContextDialer") // x/net's socks5 always implements it
		}
		return d.DialContext(ctx, network, target)
	}
	return t
}

// The SOCKS dialer is rebuilt whenever the listener address changes. It reads
// warpProxyAddr through this indirection rather than being fixed once at
// package init so the test seam can stage its own forwarder on a fresh
// loopback port; in production the address never changes and the first
// dialer simply lives forever. proxy.SOCKS5 does no I/O — building one costs
// nothing next to the dial it performs.
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
		panic("core: warp socks dialer: " + err.Error()) // malformed address only
	}
	return d
}

// ErrWARPDown reports the WARP sidecar's loopback listener refusing
// connections — the sidecar is restarting, crashed, or not enrolled yet. The
// untrusted lane fails closed on it: callers answer "limited", never a direct
// dial. Detection is exact rather than heuristic because of how SOCKS egress
// works: origin-side refusals travel back through the tunnel as SOCKS reply
// codes, so a TCP ECONNREFUSED on our side can only ever be the loopback
// listener itself refusing.
var ErrWARPDown = errors.New("warp sidecar unreachable")

// maxRedirectHops caps redirect following on every lane. Three is generous for
// legitimate API answers (none of today's providers are redirected at all);
// beyond it a URL is a redirect chain, which for a broadcaster-authored target
// is a pivot attempt, not data.
const maxRedirectHops = 3

var (
	// ErrTooManyRedirects is returned past maxRedirectHops hops.
	ErrTooManyRedirects = errors.New("redirect limit exceeded")
	// ErrHTTPSDowngrade is a redirect stepping https→http. The SSRF gate only
	// admits https origins, so a downgrade is the gate being walked around.
	ErrHTTPSDowngrade = errors.New("https to http redirect downgrade")
)

// redirectPolicy is every client's CheckRedirect: re-run the full SSRF gate on
// each hop's host (a redirect is the upstream choosing a NEW dial target, so
// it needs the same scrutiny as the original URL), forbid downgrades, cap the
// chain. Returning these typed errors surfaces through http.Client.Do wrapped
// in *url.Error, unwrapped again by errors.Is at the mapping layer.
func redirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirectHops {
		return fmt.Errorf("%w after %d hops", ErrTooManyRedirects, len(via))
	}
	if prev := via[len(via)-1].URL.Scheme; prev == "https" && req.URL.Scheme == "http" {
		return fmt.Errorf("%w to %s", ErrHTTPSDowngrade, req.URL.Host)
	}
	return SSRFCheck(req.URL)
}

// The gate's philosophy: NAMES ARE UNTRUSTED HINTS, ADDRESSES ARE THE TRUTH.
// No hostname list can be complete — naming conventions multiply (mDNS,
// cluster DNS domains, home.arpa, whatever the next RFC coins), and any name
// only ever connects as an address. So there is exactly ONE security
// invariant, stated once and enforced where addresses exist: a user-authored
// fetch may target GLOBAL UNICAST space and nothing else (classifyAddr).
// It runs at resolution/dial time on both lanes and again per redirect hop.
// SSRFCheck below is deliberately dumb — transport shape plus a literal-IP
// fast-fail so policy refusals get clean taxonomy before DNS — because every
// hostname, however it is spelled, ends at classifyAddr.
func SSRFCheck(u *url.URL) error {
	if allowPlainHTTPUpstreamsForTests.Load() {
		return nil
	}
	if u == nil || u.Host == "" {
		return &SSRFError{"missing host"}
	}
	if u.Scheme != "https" {
		return &SSRFError{fmt.Sprintf("scheme %q not allowed (https only)", u.Scheme)}
	}
	if p := u.Port(); p != "" && p != "443" {
		return &SSRFError{fmt.Sprintf("port %q not allowed (443 only)", p)}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return &SSRFError{"missing host"}
	}
	// Literal IPs need no resolver: classify immediately so obviously local
	// targets (127.0.0.1, 169.254.169.254, [::1]) come back as policy denials
	// rather than dial errors. Names defer to resolveAllowed at dial time —
	// including inet_aton shorthands ("127.1"), which Go's resolver treats as
	// ordinary domains and which therefore cannot smuggle an address past us.
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

// allowPlainHTTPUpstreamsForTests flips the gate off for the whole process.
// It exists for sibling packages' unit tests, whose fake upstreams live on
// plain-http loopback httptest servers the gate rightly refuses — the same
// fakes that predate the gate, kept byte-identical so the .Trusted() migration
// stays a provably neutral diff. Production binaries never call this (grep is
// the review: every caller lives in a _test.go), and the gate's own semantics
// stay pinned by core's table tests and the custom provider's denial test,
// which deliberately do not flip it.
var allowPlainHTTPUpstreamsForTests atomic.Bool

// SetSSRFCheckForTests pins the pre-dial gate on or off for the rest of the
// process. See allowPlainHTTPUpstreamsForTests for who may call this.
func SetSSRFCheckForTests(enabled bool) { allowPlainHTTPUpstreamsForTests.Store(!enabled) }

// The single invariant lives here: ALLOW GLOBAL UNICAST, REFUSE EVERYTHING
// ELSE. One predicate (classifyAddr), one resolver path (resolveAllowed),
// consumed by both lanes — so a new naming convention needs zero code: any
// name that resolves into public space works, any name that resolves into
// local/private/reserved space dies, whatever it is called.

// ErrBlockedAddress is a POLICY refusal minted at resolution/dial time: the
// name passed every shape rule but its address(es) are not global unicast.
// Typed so callers map it to "denied" rather than "timed out", and so the
// breaker never counts it (no socket was opened; nothing about reachability
// was learned).
type ErrBlockedAddress struct {
	Addr netip.Addr
}

// ErrBlockedAddressPolicy is the errors.Is sentinel for any *ErrBlockedAddress,
// regardless of which address triggered it.
var ErrBlockedAddressPolicy = errors.New("address is not global unicast (blocked local/private/reserved space)")

func (e *ErrBlockedAddress) Error() string {
	return "address " + e.Addr.String() + ": " + ErrBlockedAddressPolicy.Error()
}

// Is makes errors.Is(err, ErrBlockedAddressPolicy) match every instance, so
// callers classify policy refusals without string matching.
func (e *ErrBlockedAddress) Is(target error) bool { return target == ErrBlockedAddressPolicy }

// blockedSpecialV4 / blockedSpecialV6 are the IANA special-purpose ranges
// NOT already covered by Go's Is* predicates (private, loopback, link-local,
// multicast, unspecified): shared address space (CGNAT), protocol
// assignments, benchmarking, documentation nets, reserved/future. DATA, not
// logic — extending coverage when IANA carves up new space is appending a
// prefix here, not touching a code path.
var (
	blockedSpecialV4 = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
		netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
		netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
		netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
		netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast (deprecated)
		netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
		netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
		netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
		netip.MustParsePrefix("240.0.0.0/4"),     // reserved (incl. broadcast)
	}
	blockedSpecialV6 = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/96"),  // NAT64 — embeds an IPv4 we must judge
		netip.MustParsePrefix("100::/64"),      // discard-only
		netip.MustParsePrefix("2001::/32"),     // Teredo
		netip.MustParsePrefix("2001:db8::/32"), // documentation
		netip.MustParsePrefix("2002::/16"),     // 6to4 — embeds an IPv4 we must judge
	}
)

// classifyAddr is THE security predicate the whole gate reduces to. An
// address may leave the cluster only if it is global unicast AND outside
// every IANA special-purpose range. Translation formats (NAT64, 6to4,
// IPv4-mapped) are unwrapped and their EMBEDDED IPv4 re-judged, because a
// hostile author's real destination hides in the low bits, not the wrapper.
// Returns nil when the address may be dialed.
func classifyAddr(a netip.Addr) error {
	if !a.IsValid() {
		return fmt.Errorf("invalid address")
	}
	a = a.Unmap()
	switch {
	case a.IsUnspecified():
		return fmt.Errorf("unspecified address")
	case a.IsLoopback():
		return fmt.Errorf("loopback address")
	case a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast(), a.IsInterfaceLocalMulticast(), a.IsMulticast():
		return fmt.Errorf("link-local/multicast address")
	case a.IsPrivate():
		return fmt.Errorf("private (RFC1918/ULA) address")
	case a.Is4():
		if p, ok := matchedPrefix(a, blockedSpecialV4); ok {
			return fmt.Errorf("special-purpose range %s", p)
		}
	case a.Is6():
		if v4, ok := embeddedV4(a); ok {
			return classifyAddr(v4)
		}
		if p, ok := matchedPrefix(a, blockedSpecialV6); ok {
			return fmt.Errorf("special-purpose range %s", p)
		}
	default:
		return fmt.Errorf("unroutable address family")
	}
	return nil
}

// embeddedV4 extracts the IPv4 carried by IPv4-mapped (::ffff:/96), NAT64
// (64:ff9b::/96) or 6to4 (2002::/16) addresses; false for everything else.
// The formats differ in WHERE the IPv4 sits: mapped and NAT64 carry it in the
// low 32 bits, but 6to4 (RFC 3056) embeds it in bits 16-48 — reading the low
// bytes there judges attacker-chosen decoys (2002:a00:1::808:808 wraps
// 10.0.0.1 while its low bytes read 8.8.8.8), which is exactly the bug this
// split fixes.
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

// lowBitsEmbedV4 lists the translation formats carrying their IPv4 in the low
// 32 bits (6to4 carries it in bits 16-48 instead — see embeddedV4).
var lowBitsEmbedV4 = []netip.Prefix{
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
}

// matchedPrefix returns the first prefix containing a.
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

// resolveAllowed resolves ONE hostname under the strict rule: EVERY returned
// address must classify as dialable — a single poisoned record rejects the
// whole name. That is what closes mixed-record DNS rebinding: an attacker
// controlling a zone can alternate public and 169.254.x answers, and letting
// the dialer pick would race them. Literals skip resolution and are judged
// directly. The returned address is PINNED — callers dial exactly it, so no
// second lookup exists between verdict and connect. The TOCTOU window IS the
// rebinding attack; pinning deletes it.
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

// allowedAddr admits one already-parsed address, or refuses it as policy.
func allowedAddr(a netip.Addr) (netip.Addr, error) {
	if verr := classifyAddr(a); verr != nil {
		return netip.Addr{}, &ErrBlockedAddress{Addr: a}
	}
	return a, nil
}

// firstAllowed applies the every-record-must-pass rule and returns the first
// address, preserving the resolver's ordering preference.
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

// SSRFError is the typed refusal SSRFCheck returns, so callers can tell a
// policy denial apart from infrastructure failure without string matching.
type SSRFError struct{ Reason string }

func (e *SSRFError) Error() string { return "url rejected: " + e.Reason }

// customMaxBody bounds a USER-defined fetch's body read. The limit lands on
// io.LimitReader AFTER net/http's transparent gzip decompression — that is the
// gzip-bomb guard: 1 MiB claimed becomes whatever it inflates to and THIS
// reader stops it. Content-Length is never consulted (it lies). Not
// http.MaxBytesReader: that is request-shaped and reports 413-flavored errors
// about the caller's own payload, matching decodeJSON's LimitReader precedent.
// The +1 detects overflow: a full read means there was more.
const customMaxBody = 1<<20 + 1

// FetchBounded performs one gate-checked round trip and returns up to 1 MiB of
// the response body, enforcing the content-type allowlist. It exists for the
// custom urlfetch provider, whose targets are broadcaster-authored and whose
// bodies are never decoded into a struct: Do's JSON path has no place to put
// these checks without taxing every trusted provider with them.
func (c *HTTPClient) FetchBounded(ctx context.Context, r Request) ([]byte, error) {
	req, err := c.newRequest(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := SSRFCheck(req.URL); err != nil {
		return nil, err
	}

	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, customMaxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &UpstreamError{Status: resp.StatusCode, Message: upstreamMessage(body)}
	}
	ct := resp.Header.Get("Content-Type")
	if !allowedContentType(ct) {
		return nil, fmt.Errorf("%w: %q", ErrContentTypeNotAllowed, ct)
	}
	if len(body) > customMaxBody-1 {
		return nil, fmt.Errorf("response exceeds %d bytes: %w", customMaxBody-1, ErrBodyTooLarge)
	}
	return body, nil
}

// The two post-response refusals below are typed so the caller can tell "the
// upstream answered but its payload is unusable" apart from a transport
// failure — which matters because the urlfetch breaker counts CONNECT/TIMEOUT
// failures only: an answered request proves the host reachable and must reset
// the circuit, even when we refuse to parse what it sent.
var (
	// ErrContentTypeNotAllowed is a response outside application/json + text/*.
	ErrContentTypeNotAllowed = errors.New("content type not allowed")
	// ErrBodyTooLarge is a body still coming after the post-decompression cap.
	ErrBodyTooLarge = errors.New("response body too large")
)

// allowedContentType reports whether a response Content-Type may be parsed for
// a user-defined fetch. application/json plus text/*: the two shapes the
// extractor understands, and nothing a hostile upstream could smuggle active
// content into a chat line with. A missing header is refused — strictness is
// cheap when the author controls the endpoint choice.
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
	// The SSRF gate runs before EVERY dial on BOTH lanes — trusted hosts get
	// the same host-shape check for uniformity, and it guards a misconfigured
	// trusted base URL too. Microsecond cost.
	if err := SSRFCheck(req.URL); err != nil {
		return err
	}

	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON(resp, out)
}

// roundTrip is the one dial path both entry points share: the New Relic
// external segment (so "the provider is slow" is always attributable), the
// lane attribute on WARP-lane segments (wrong-lane egress is visible in the
// trace that owns the whole picture), and the typed ErrWARPDown wrap that
// makes a dead sidecar distinguishable from any other failure.
func (c *HTTPClient) roundTrip(ctx context.Context, req *http.Request) (*http.Response, error) {
	// Report the call to New Relic as an external segment. Without this a
	// handler's transaction is one opaque block, so "the provider is slow" and
	// "we are slow" are indistinguishable in the only place that has the whole
	// picture — which is exactly the question a slow !fnstats raises, and one
	// that cost a manual round of curl-from-a-probe-pod to answer once. The
	// segment ends before the body is read, so it measures the upstream's time
	// to first byte and not our decode; decodeJSON's read and unmarshal stay
	// in the surrounding transaction, which is where they belong.
	segment := newrelic.StartExternalSegment(newrelic.FromContext(ctx), req)
	if c.lane == LaneWARP {
		segment.AddAttribute("lane", "warp")
	}
	resp, err := c.hc.Do(req)
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
	// A 204 carries no body by definition, so there is nothing to unmarshal:
	// leave out zero-valued and report success. Falling through used to
	// surface as a confusing decode error indistinguishable from an upstream
	// fault; Spotify's currently-playing endpoint answers 204 for "nothing
	// playing", which is an answer, not a failure.
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
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
