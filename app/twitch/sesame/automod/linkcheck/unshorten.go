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

var DefaultShorteners = []string{
	"bit.ly", "t.co", "tinyurl.com", "goo.gl", "ow.ly", "is.gd",
	"buff.ly", "cutt.ly", "rb.gy", "t.ly", "rebrand.ly", "shorturl.at",
	"tiny.cc", "bit.do", "v.gd", "x.gd", "s.id", "snip.ly", "lnkd.in",
	"clck.ru", "soo.gd",
}

const (
	maxRedirectHops  = 5
	expandBudget     = 6 * time.Second
	hopTimeout       = 3 * time.Second
	discardBodyLimit = 64 << 10
)

type Expander struct {
	client  *http.Client
	short   map[string]struct{}
	scheme  string
	anyPort bool
}

func NewExpander(client *http.Client, shorteners []string) *Expander {
	return newExpanderScheme(client, shorteners, "https")
}

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
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &Expander{client: client, short: short, scheme: scheme, anyPort: scheme != "https"}
}

func isWebPort(p string) bool { return p == "" || p == "80" || p == "443" }

func (e *Expander) IsShortener(host string) bool {
	_, ok := e.short[host]
	return ok
}

func (e *Expander) Destination(ctx context.Context, token string) (string, error) {
	current, err := url.Parse(e.scheme + "://" + strings.ToLower(token))
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", token, err)
	}

	ctx, cancel := context.WithTimeout(ctx, expandBudget)
	defer cancel()

	for hop := 0; ; hop++ {
		if host := e.externalHost(current); host != "" {
			return host, nil
		}
		if hop >= maxRedirectHops {
			return "", fmt.Errorf("expand %s: exceeded %d hops", token, maxRedirectHops)
		}
		next, err := e.nextHop(ctx, current)
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", token, err)
		}
		if next == nil {
			return strings.ToLower(current.Hostname()), nil
		}
		current = next
	}
}

func (e *Expander) externalHost(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if e.mayProbe(host, u.Port()) {
		return ""
	}
	return host
}

func (e *Expander) mayProbe(host, port string) bool {
	return e.IsShortener(host) && (e.anyPort || isWebPort(port))
}

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

func (e *Expander) probe(ctx context.Context, rawURL string) (location string, err error) {
	// Must stay GET: several shortener edges mishandle HEAD, minting a false Clean.
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
