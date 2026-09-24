// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://discord.com/api/v10"

	requestTimeout = 5 * time.Second

	maxBodyBytes      = 1 << 20
	maxErrorBodyBytes = 2048
)

type Client struct {
	http  *http.Client
	base  string
	token string
}

var (
	ErrAuth            = errors.New("discord: unauthorized")
	ErrForbidden       = errors.New("discord: forbidden")
	ErrChannelNotFound = errors.New("discord: channel not found")
	ErrBadRequest      = errors.New("discord: bad request")
	ErrRateLimited     = errors.New("discord: rate limited")
)

func NewClient(botToken string) *Client {
	return &Client{
		http:  &http.Client{Timeout: requestTimeout},
		base:  defaultBaseURL,
		token: botToken,
	}
}

func (c *Client) SetTransport(rt http.RoundTripper) { c.http.Transport = rt }

type ChatPost struct {
	ChannelID string
	Content   string
	TTS       bool
}

func (c *Client) SendChat(ctx context.Context, post ChatPost) error {
	return c.SendMessage(ctx, post.ChannelID, post.Content, post.TTS)
}

func (c *Client) SendMessage(ctx context.Context, channelID, content string, tts bool) error {
	body := map[string]any{"content": content}
	if tts {
		body["tts"] = true
	}
	return c.do(ctx, request{method: http.MethodPost, path: "/channels/" + url.PathEscape(channelID) + "/messages", body: body})
}

type request struct {
	method string
	path   string
	body   any
	reason string
}

func (r request) payload() ([]byte, error) {
	if r.body == nil {
		return nil, nil
	}
	raw, err := codec.Marshal(r.body)
	if err != nil {
		return nil, fmt.Errorf("discord: encode body: %w", err)
	}
	return raw, nil
}

func (c *Client) do(ctx context.Context, req request) error {
	_, err := c.doBytes(ctx, req)
	return err
}

func (c *Client) doInto(ctx context.Context, req request, out any) error {
	raw, err := c.doBytes(ctx, req)
	if err != nil {
		return err
	}
	if err := codec.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("discord: decode %s %s: %w", req.method, req.path, err)
	}
	return nil
}

func (c *Client) doBytes(ctx context.Context, req request) ([]byte, error) {
	payload, err := req.payload()
	if err != nil {
		return nil, err
	}
	res, err := c.send(ctx, req, payload)
	if err != nil {
		return nil, err
	}
	defer drain(res)

	raw := readBody(res)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return raw, nil
	}
	return nil, classify(res, raw)
}

func (c *Client) send(ctx context.Context, req request, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, c.base+req.path, body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bot "+c.token)
	if req.reason != "" {
		httpReq.Header.Set("X-Audit-Log-Reason", auditReason(req.reason))
	}
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(httpReq)
}

const auditReasonMax = 512

func auditReason(reason string) string {
	r := []rune(reason)
	if len(r) > auditReasonMax {
		r = r[:auditReasonMax]
	}
	return url.PathEscape(string(r))
}

func classify(res *http.Response, raw []byte) error {
	detail := errorDetail(raw)
	switch res.StatusCode {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, detail)
	case http.StatusUnauthorized:
		return ErrAuth
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, detail)
	case http.StatusNotFound:
		return ErrChannelNotFound
	case http.StatusTooManyRequests:
		return newRateLimitError(res, raw, detail)
	}
	return fmt.Errorf("discord: api rejected request (%d): %s", res.StatusCode, detail)
}

func errorDetail(raw []byte) string {
	if len(raw) > maxErrorBodyBytes {
		raw = raw[:maxErrorBodyBytes]
	}
	return string(raw)
}

type RateLimitError struct {
	RetryAfter time.Duration
	Global     bool
	detail     string
}

func newRateLimitError(res *http.Response, raw []byte, detail string) *RateLimitError {
	e := &RateLimitError{detail: detail}
	var body struct {
		RetryAfter float64 `json:"retry_after"`
		Global     bool    `json:"global"`
	}
	if err := codec.Unmarshal(raw, &body); err == nil {
		e.RetryAfter = time.Duration(body.RetryAfter * float64(time.Second))
		e.Global = body.Global
	}
	if secs, err := strconv.ParseFloat(res.Header.Get("Retry-After"), 64); err == nil && secs > 0 {
		e.RetryAfter = time.Duration(secs * float64(time.Second))
	}
	return e
}

func (e *RateLimitError) Error() string { return "discord: rate limited: " + e.detail }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

func RetryAfterOf(err error) time.Duration {
	var rl *RateLimitError
	if errors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}

func readBody(res *http.Response) []byte {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	return raw
}

func drain(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxBodyBytes+1)
	_ = res.Body.Close()
}
