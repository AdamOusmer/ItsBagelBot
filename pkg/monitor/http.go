// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"net/http"
	"time"

	"github.com/newrelic/go-agent/v3/newrelic"
)

func HTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: ExternalTransport(nil), Timeout: timeout}
}

type externalTransport struct {
	base http.RoundTripper
}

// Third-party hosts must never receive trace headers: they carry the New Relic account and app ids.
func ExternalTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return externalTransport{base: base}
}

func (t externalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	txn := newrelic.FromContext(req.Context())
	if txn == nil {
		return t.base.RoundTrip(req)
	}
	segment := newrelic.ExternalSegment{
		StartTime: txn.StartSegmentNow(),
		URL:       req.URL.Scheme + "://" + req.URL.Host + req.URL.Path,
		Procedure: req.Method,
		Library:   "http",
	}
	resp, err := t.base.RoundTrip(req)
	segment.Response = resp
	segment.End()
	return resp, err
}
