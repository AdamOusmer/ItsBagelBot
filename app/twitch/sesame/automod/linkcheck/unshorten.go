// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// DefaultShorteners is the allowlist of hosts whose redirect headers the
// expander may walk. Deliberately the mainstream set chat actually carries —
// every entry here is an outbound-request permission, so the bar for adding
// one is "viewers genuinely paste this", not "it redirects". Hosts already on
// the immovable floor (yip.su et al) are absent: the floor bans those lines
// inline before any expansion could matter.
var DefaultShorteners = []string{
	"bit.ly", "t.co", "tinyurl.com", "goo.gl", "ow.ly", "is.gd",
	"buff.ly", "cutt.ly", "rb.gy", "t.ly", "rebrand.ly", "shorturl.at",
	"tiny.cc", "bit.do", "v.gd", "x.gd", "s.id", "snip.ly", "lnkd.in",
	"clck.ru", "soo.gd",
}

// Expansion envelope constants, recorded so they can be re-argued:
//
//   - maxRedirectHops 5: live chains measure 1-3 hops (bit.ly -> t.co -> dest
//     is the deep case); five leaves headroom without letting a redirect loop
//     spin. A chain that needs more is not something we want to resolve anyway.
//   - expandBudget 6s total per walk: three hops x ~2s worst-case hop latency,
//     bounded once so a slow shortener cannot hold a worker slot open all day.
//   - hopTimeout 3s per request: covers TCP + TLS + headers against the big
//     shortener CDN edges; anything slower is down, and down means unknown,
//     which means no verdict rather than a wrong one.
//   - discardBodyLimit 64KB: bodies are never parsed or stored — only status
//     line and Location matter — but some edges stream error pages before
//     honoring connection close, so a capped drain prevents socket stalls.
const (
	maxRedirectHops  = 5
	expandBudget     = 6 * time.Second
	hopTimeout       = 3 * time.Second
	discardBodyLimit = 64 << 10
)

// Expander resolves shortener links to their destination host by walking
// redirect HEADERS ONLY across the allowlist. The safety shape, in order:
//
//  1. Requests go to allowlisted shortener hosts exclusively. When a Location
//     leaves the allowlist, that target IS the destination — it is returned to
//     the caller uncontacted. Attacker-controlled pages never receive a request
//     from sesame, see no IP, log no hit.
//  2. Belt-and-braces against DNS rebinding (a shortener name resolving inward):
//     the dialer's Control hook refuses any resolved address in loopback,
//     private, link-local, CGNAT, ULA or multicast space before the socket even
//     opens. This is process-level containment only; at the infra layer the
//     pod's egress should still sit behind a default-deny policy (WARP/Gateway)
//     allowing just this package's oracle endpoints — the two layers fail
//     different ways and neither assumes the other.
//  3. GET (not HEAD): several shortener edges mishandle HEAD, and a wrong
//     answer here mints a false Clean. Bodies are drained capped and discarded;
//     nothing from them is read.
type Expander struct {
	client  *http.Client
	short   map[string]struct{}
	scheme  string // "https" in production; httptest needs plain http
	anyPort bool   // test mode: match membership on name, not name+web-port
}

// NewExpander builds the walker around shorteners (empty selects
// DefaultShorteners) and an optional client; nil gets the guarded default.
func NewExpander(client *http.Client, shorteners []string) *Expander {
	return newExpanderScheme(client, shorteners, "https")
}

// newExpanderScheme exists for tests: httptest endpoints speak plain HTTP on
// random ports, while every mainstream shortener is HTTPS-only on web ports,
// so production keeps both strictures.
func newExpanderScheme(client *http.Client, shorteners []string, scheme string) *Expander {
	if len(shorteners) == 0 {
		shorteners = DefaultShorteners
	}
	short := make(map[string]struct{}, len(shorteners))
	for _, s := range shorteners {
		short[strings.ToLower(s)] = struct{}{}
	}
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   hopTimeout,
					KeepAlive: hopTimeout,
					Control:   guardDial,
				}).DialContext,
				TLSHandshakeTimeout: hopTimeout,
				MaxIdleConns:        4,
			},
			// Never auto-follow: the manual loop below decides hop by hop
			// whether the next target may be contacted at all.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Expander{client: client, short: short, scheme: scheme, anyPort: scheme != "https"}
}

// isWebPort reports whether p is empty or one of the two ports a chat link
// can implicitly use. Anything explicit is exotic by definition here.
func isWebPort(p string) bool { return p == "" || p == "80" || p == "443" }

// IsShortener reports whether host sits on the allowlist.
func (e *Expander) IsShortener(host string) bool {
	_, ok := e.short[host]
	return ok
}

// Destination walks the redirect chain of rawURL (a token as iterLinkTokens
// yields it, e.g. "bit.ly/abc") and returns the first destination HOST outside
// the allowlist — contacted by nobody. An input host that is not a shortener
// returns immediately with itself (the caller classified plain hosts directly);
// chains that stay inside the allowlist through maxRedirectHops return an
// error rather than a guess. The chain travels as parsed URLs so each hop's
// hostname comes from the parser, not from token slicing — hostOf is
// token-shaped and would misread a full URL's scheme as its host.
func (e *Expander) Destination(ctx context.Context, token string) (string, error) {
	current, err := url.Parse(e.scheme + "://" + strings.ToLower(token))
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", token, err)
	}

	ctx, cancel := context.WithTimeout(ctx, expandBudget)
	defer cancel()

	for hop := 0; ; hop++ {
		if host := e.externalHost(current); host != "" {
			return host, nil // the destination: returned, never requested
		}
		if hop >= maxRedirectHops {
			return "", fmt.Errorf("expand %s: exceeded %d hops", token, maxRedirectHops)
		}
		next, err := e.nextHop(ctx, current)
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", token, err)
		}
		if next == nil {
			// Non-redirect response (landing page, interstitial, bot wall):
			// the shortener's own host is what we can honestly report.
			return strings.ToLower(current.Hostname()), nil
		}
		current = next
	}
}

// externalHost reports the destination hostname when u sits outside the
// expansion envelope, or "" when u is a shortener this package may probe.
// The hostname must sit on the allowlist AND the port must be web-standard —
// "bit.ly:8443" cannot inherit "bit.ly"'s permission and travels nowhere.
// anyPort (test mode, riding the same constructor switch as plain http)
// matches on name alone because httptest binds random ports.
func (e *Expander) externalHost(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if e.mayProbe(host, u.Port()) {
		return ""
	}
	return host
}

// mayProbe reports whether this package may issue a request to host: it must be
// an allowlisted shortener reached on a web-standard port. anyPort (test mode)
// waives the port check because httptest binds random ports.
func (e *Expander) mayProbe(host, port string) bool {
	return e.IsShortener(host) && (e.anyPort || isWebPort(port))
}

// nextHop resolves one redirect step: probe the current URL, parse its
// Location relative to it, refuse exotic schemes. A nil URL with a nil error
// means the response was not a redirect. Callers only reach here for hosts
// externalHost cleared.
func (e *Expander) nextHop(ctx context.Context, current *url.URL) (*url.URL, error) {
	loc, err := e.probe(ctx, current.String())
	if err != nil || loc == "" {
		return nil, err
	}
	ref, err := current.Parse(loc)
	if err != nil {
		return nil, fmt.Errorf("bad location: %w", err)
	}
	if ref.Scheme != "https" && ref.Scheme != "http" {
		return nil, fmt.Errorf("scheme %q refused", ref.Scheme)
	}
	return ref, nil
}

// probe issues one capped GET and returns the next Location header value (""
// when the response is not a redirect).
func (e *Expander) probe(ctx context.Context, rawURL string) (location string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	res, err := e.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, discardBodyLimit))
		_ = res.Body.Close()
	}()
	if res.StatusCode < 300 || res.StatusCode > 399 {
		return "", nil
	}
	return strings.TrimSpace(res.Header.Get("Location")), nil
}

// guardDial is the transport-level SSRF gate applied to every dialed address:
// it runs after DNS resolution, before connect, so a rebinding shortener name
// pointing at link-local metadata or cluster-private space is refused at the
// socket layer regardless of what the allowlist believed about the hostname.
func guardDial(_ string, address string, _ syscall.RawConn) error {
	h, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("linkcheck dial guard: %w", err)
	}
	ip := net.ParseIP(h)
	switch {
	case ip == nil:
		return fmt.Errorf("linkcheck dial guard: unparseable address %q", h)
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsUnspecified(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsMulticast():
		return fmt.Errorf("linkcheck dial guard: refused internal address %s", h)
	}
	// CGNAT 100.64/10 shares private-space semantics on modern clusters and is
	// not covered by IsPrivate; ULA fc00::/7 likewise on the IPv6 side.
	if isCGNAT(ip) || isULA(ip) {
		return fmt.Errorf("linkcheck dial guard: refused internal address %s", h)
	}
	return nil
}

func isCGNAT(ip net.IP) bool {
	return ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] < 128
}

func isULA(ip net.IP) bool {
	return len(ip) == 16 && ip[0]&0xfe == 0xfc
}
