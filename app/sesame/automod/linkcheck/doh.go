// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
)

// DefaultDoHEndpoint is Cloudflare's security-filtering resolver (1.1.1.2's
// DoH form). A domain on its malware/phishing blocklist resolves to 0.0.0.0
// instead of the real address — that answer IS the verdict signal, which makes
// this a passive oracle: we resolve, we never connect. Free, unauthenticated,
// one small JSON GET per lookup; overridable so tests point it at httptest.
const DefaultDoHEndpoint = "https://security.cloudflare-dns.com/dns-query"

// dohTimeout bounds one resolver query. Measured p95 for the public resolvers
// is tens of milliseconds; three seconds absorbs transatlantic jitter without
// letting one cold query stall the worker queue behind it.
const dohTimeout = 3 * time.Second

// dohBodyLimit caps the response read. A real answer is well under 1KB; the
// cap only matters when endpoint != default (tests, self-hosted gateways).
const dohBodyLimit = 64 << 10

// DoH asks a DNS-over-HTTPS JSON endpoint whether a host sits on its security
// blocklist. Safe by construction: resolution only — no HTTP connection is
// ever opened to the classified host.
type DoH struct {
	endpoint string
	client   *http.Client
}

// NewDoH builds a resolver oracle. Empty endpoint selects DefaultDoHEndpoint;
// nil client gets the bounded default.
func NewDoH(endpoint string, client *http.Client) *DoH {
	if endpoint == "" {
		endpoint = DefaultDoHEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: dohTimeout}
	}
	return &DoH{endpoint: endpoint, client: client}
}

// dohAnswer mirrors the fields of RFC 8484-style JSON responses this check
// reads. The wire format carries more (Authority, TTLs); anything unmapped is
// ignored on purpose.
type dohAnswer struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

// Blocked reports whether the resolver's security layer blocks host. A blocked
// domain answers with a 0.0.0.0 / :: A or AAAA record; that sinkhole answer is
// the entire detection surface. NXDOMAIN reads as not-blocked (an unresolvable
// host cannot route a victim anywhere either) rather than as an error: treating
// registry hiccups as errors would burn the retry cooldown on every typo'd
// hostname chat invents.
func (d *DoH) Blocked(ctx context.Context, host string) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	doc, err := d.resolve(cctx, host)
	if err != nil {
		return false, err
	}
	return sinkholed(doc), nil
}

// resolve issues the security-DoH query for host and decodes its answer set.
func (d *DoH) resolve(ctx context.Context, host string) (dohAnswer, error) {
	var doc dohAnswer
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		d.endpoint+"?name="+url.QueryEscape(host)+"&type=A", nil)
	if err != nil {
		return doc, err
	}
	req.Header.Set("accept", "application/dns-json")

	res, err := d.client.Do(req)
	if err != nil {
		return doc, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, dohBodyLimit))
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return doc, fmt.Errorf("doh %s: status %d", host, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, dohBodyLimit))
	if err != nil {
		return doc, fmt.Errorf("doh %s: %w", host, err)
	}
	if err := codec.Unmarshal(body, &doc); err != nil {
		return doc, fmt.Errorf("doh %s: %w", host, err)
	}
	return doc, nil
}

// sinkholed reports whether any A/AAAA record is the resolver's block answer
// (0.0.0.0 / ::) — the entire detection surface.
func sinkholed(doc dohAnswer) bool {
	for _, a := range doc.Answer {
		if a.Type != 1 && a.Type != 28 { // A, AAAA
			continue
		}
		switch strings.TrimSuffix(a.Data, ".") {
		case "0.0.0.0", "::":
			return true
		}
	}
	return false
}
