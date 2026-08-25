// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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

	req, err := http.NewRequestWithContext(cctx, http.MethodGet,
		d.endpoint+"?name="+url.QueryEscape(host)+"&type=A", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("accept", "application/dns-json")

	res, err := d.client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, dohBodyLimit))
		_ = res.Body.Close()
	}()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("doh %s: status %d", host, res.StatusCode)
	}

	var doc dohAnswer
	if err := json.NewDecoder(io.LimitReader(res.Body, dohBodyLimit)).Decode(&doc); err != nil {
		return false, fmt.Errorf("doh %s: %w", host, err)
	}
	for _, a := range doc.Answer {
		if a.Type == 1 || a.Type == 28 { // A, AAAA
			data := strings.TrimSuffix(a.Data, ".")
			if data == "0.0.0.0" || data == "::" {
				return true, nil
			}
		}
	}
	return false, nil
}
