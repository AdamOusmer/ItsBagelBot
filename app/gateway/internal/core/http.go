package core

import (
	"bytes"
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
	resp, err := c.hc.Do(req)
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
	req.Header.Set("User-Agent", "ItsBagelBot-gateway/1.0")
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
	return json.Unmarshal(body, out)
}

// upstreamMessage pulls the upstream's own error text from a JSON error body,
// tolerating either the fleet's "error" field or Govee's "message" field.
func upstreamMessage(body []byte) string {
	var envelope struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &envelope)
	if envelope.Error != "" {
		return envelope.Error
	}
	return envelope.Message
}
