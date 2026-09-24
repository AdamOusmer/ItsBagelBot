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

const DefaultDoHEndpoint = "https://security.cloudflare-dns.com/dns-query"

const dohTimeout = 3 * time.Second

const (
	dohBodyLimit = 64 << 10

	dnsTypeA    = 1
	dnsTypeAAAA = 28
)

type DoH struct {
	endpoint string
	client   *http.Client
}

func NewDoH(endpoint string, client *http.Client) *DoH {
	if endpoint == "" {
		endpoint = DefaultDoHEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: dohTimeout}
	}
	return &DoH{endpoint: endpoint, client: client}
}

type dohAnswer struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func (d *DoH) Blocked(ctx context.Context, host string) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	doc, err := d.resolve(cctx, host)
	if err != nil {
		return false, err
	}
	return sinkholed(doc), nil
}

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

func sinkholed(doc dohAnswer) bool {
	for _, a := range doc.Answer {
		if a.Type != dnsTypeA && a.Type != dnsTypeAAAA {
			continue
		}
		switch strings.TrimSuffix(a.Data, ".") {
		case "0.0.0.0", "::":
			return true
		}
	}
	return false
}
