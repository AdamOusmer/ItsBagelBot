package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// maxBody bounds an upstream response read. The largest legitimate payload the
// gateway handles is a full Hypixel profile (a few hundred KB); anything past
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
}

func (e *UpstreamError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("upstream %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("upstream status %d", e.Status)
}

// HTTPClient is the outbound fetcher a provider dials its API with: one base
// URL, a fixed header set (API keys), and a bounded per-request timeout.
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
	return &HTTPClient{base: base, headers: headers, hc: &http.Client{Timeout: timeout}}
}

// GetJSON fetches base+path?query and decodes the JSON body into out. A non-2xx
// status returns an *UpstreamError carrying the upstream's own error message
// when it sent one.
func (c *HTTPClient) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ItsBagelBot-gateway/1.0")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var envelope struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &envelope)
		return &UpstreamError{Status: resp.StatusCode, Message: envelope.Error}
	}

	return json.Unmarshal(body, out)
}
