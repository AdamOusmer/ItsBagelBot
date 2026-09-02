// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package discord is the Discord REST API v10 client. Outgress uses it for
// live embeds, clip posts, and the 1-click guild fill; dingress uses the
// same types for welcomes, auto-voice, and slash replies. Every call
// authenticates with the static bot token, which never rotates through
// outgress: resetting bot credentials on the Discord developer portal
// invalidates the token instantly and takes every send down until the env
// var is updated and the pod restarts.
//
// Error classification is the load-bearing contract: the worker maps these
// onto the lane's ack/nack discipline, where a wrong class either spins a
// dead message forever or silently drops a retryable one.
package discord

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

	// requestTimeout bounds one REST call end to end. Sends are perishable
	// (the lane's MaxAge is 5s), so a hung call must fail fast enough that the
	// ack/nack decision still lands inside the message's useful life.
	requestTimeout = 5 * time.Second

	// maxBody caps a success body. A guild channel listing is ~400 B per
	// channel and a message-create echo carries the author and embed back, so
	// the 2 KiB error cap silently truncated both (the id decoded empty and
	// the go-offline edit never found its message). 1 MiB admits any listing
	// Discord returns today while still bounding a runaway body.
	maxBody = 1 << 20
	// maxErrorBody bounds what an error message carries into the logs.
	maxErrorBody = 2048
)

// Client calls the Discord REST API under the bot token.
type Client struct {
	http  *http.Client
	base  string
	token string
}

// Typed errors. The worker maps each to drop (nil return) or nack (error).
var (
	// ErrAuth means the token was rejected (401): revoked by a credential
	// reset or simply wrong. Redelivery cannot succeed; this is dropped
	// loudly like a revoked Helix token.
	ErrAuth = errors.New("discord: unauthorized")
	// ErrForbidden covers 403: the bot lacks permission in the target channel,
	// was blocked, or the channel forbids the bot outright. A dashboard fix
	// (role/permission change) can make future sends work, but THIS message's
	// redelivery cannot succeed within its lifetime.
	ErrForbidden = errors.New("discord: forbidden")
	// ErrChannelNotFound (404 unknown channel): the channel was deleted or
	// the id is wrong. Permanent for this message.
	ErrChannelNotFound = errors.New("discord: channel not found")
	// ErrBadRequest (400): Discord rejected the body itself. Nothing about
	// redelivery changes the payload, so it is permanent.
	ErrBadRequest = errors.New("discord: bad request")
	// ErrRateLimited is transient pressure (429). The lane nacks and paced
	// redelivery retries it; a chat line older than its retry budget dies at
	// MaxAge instead of arriving late, which is the intended trade.
	ErrRateLimited = errors.New("discord: rate limited")
)

// NewClient builds the REST client against the production endpoint.
func NewClient(botToken string) *Client {
	return &Client{
		http:  &http.Client{Timeout: requestTimeout},
		base:  defaultBaseURL,
		token: botToken,
	}
}

// SetTransport swaps the HTTP transport (tests inject fakes here).
func (c *Client) SetTransport(rt http.RoundTripper) { c.http.Transport = rt }

// SendMessage posts one text message into a channel. tts passes through to
// Discord's TTS flag; content is validated upstream (the worker bounds it).
func (c *Client) SendMessage(ctx context.Context, channelID, content string, tts bool) error {
	body := map[string]any{"content": content}
	if tts {
		body["tts"] = true
	}
	return c.do(ctx, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/messages", body)
}

// do runs one classified API call. There is no refresh dance: the bot token
// is static, so any rejection classifies once.
func (c *Client) do(ctx context.Context, method, path string, body any) error {
	payload, err := codec.Marshal(body)
	if err != nil {
		return fmt.Errorf("discord: encode body: %w", err)
	}
	return c.call(ctx, method, path, payload)
}

func (c *Client) call(ctx context.Context, method, path string, payload []byte) error {
	_, err := c.callBytes(ctx, method, path, payload)
	return err
}

func (c *Client) callBytes(ctx context.Context, method, path string, payload []byte) ([]byte, error) {
	res, err := c.send(ctx, method, path, payload)
	if err != nil {
		return nil, err // network/transient: caller nacks
	}
	defer drain(res)

	raw := readBody(res)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return raw, nil
	}
	return nil, classify(res, raw)
}

func (c *Client) send(ctx context.Context, method, path string, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// classify maps a non-2xx status onto the typed errors above. The body is
// truncated to maxErrorBody before it reaches an error string.
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
	if len(raw) > maxErrorBody {
		raw = raw[:maxErrorBody]
	}
	return string(raw)
}

// RateLimitError carries Discord's 429 verdict: RetryAfter is the wait the
// server dictated (header first, JSON body second) and Global says whether
// the whole bot is throttled rather than one channel bucket. The setup fill
// sleeps RetryAfter before its next create; the lanes surface it in logs so
// per-channel pressure can be told from a fleet-wide bucket. It is
// classified exactly like ErrRateLimited.
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
	// errors.Is must reach ErrRateLimited both via Unwrap (for wrapped use) and
	// directly (for the worker's switch on the concrete type).
	return target == ErrRateLimited
}

// RetryAfterOf returns the server-dictated wait when err is a 429, else 0.
func RetryAfterOf(err error) time.Duration {
	var rl *RateLimitError
	if errors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}

func readBody(res *http.Response) []byte {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxBody))
	return raw
}

// drain makes small responses reusable without letting a large or
// non-terminating body pin the worker.
func drain(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxBody+1)
	_ = res.Body.Close()
}
