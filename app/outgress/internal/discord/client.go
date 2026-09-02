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
	"time"
)

const (
	defaultBaseURL = "https://discord.com/api/v10"

	// requestTimeout bounds one REST call end to end. Sends are perishable
	// (the lane's MaxAge is 5s), so a hung call must fail fast enough that the
	// ack/nack decision still lands inside the message's useful life.
	requestTimeout = 5 * time.Second

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

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err // network/transient: caller nacks
	}
	defer drain(res)

	raw := readBody(res)
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return []byte(raw), nil

	case res.StatusCode == http.StatusBadRequest:
		return nil, fmt.Errorf("%w: %s", ErrBadRequest, raw)

	case res.StatusCode == http.StatusUnauthorized:
		return nil, ErrAuth

	case res.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrForbidden, raw)

	case res.StatusCode == http.StatusNotFound:
		return nil, ErrChannelNotFound

	case res.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimitError{detail: raw}

	case res.StatusCode >= 400:
		return nil, fmt.Errorf("discord: api rejected request (%d): %s",
			res.StatusCode, raw)
	}

	return nil, fmt.Errorf("discord: unexpected api status %d", res.StatusCode)
}

// RateLimitError carries Discord's 429 body (which includes retry_after and a
// global flag) so operators can tell per-channel pressure from a fleet-wide
// bucket in the logs. It is classified exactly like ErrRateLimited.
type RateLimitError struct {
	detail string
}

func (e *RateLimitError) Error() string { return "discord: rate limited: " + e.detail }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
func (e *RateLimitError) Is(target error) bool {
	// errors.Is must reach ErrRateLimited both via Unwrap (for wrapped use) and
	// directly (for the worker's switch on the concrete type).
	return target == ErrRateLimited
}

func readBody(res *http.Response) string {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	return string(raw)
}

// drain makes small responses reusable without letting a large or
// non-terminating body pin the worker.
func drain(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxErrorBody+1)
	_ = res.Body.Close()
}
