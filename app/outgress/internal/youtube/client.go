// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package youtube is outgress's client for the YouTube Data API v3 write
// surface: sending live chat messages and issuing moderation actions
// (delete, ban, timeout). It is deliberately narrow — the ingress owns
// reading YouTube; everything here spends the project's daily quota budget,
// so every call site pays `Budget.Take` before it fires.
//
// Error classification is the load-bearing contract: the worker maps these
// onto the lane's ack/nack discipline, where a wrong class either spins a
// dead message forever or silently drops a retryable one.
package youtube

import (
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// QuotaUnitsPerAction is what Google charges per liveChatMessages.insert /
	// liveChatMessages.delete / liveChatBans.insert. Every action costs the
	// same flat 50 units, which is why one constant covers all of them.
	QuotaUnitsPerAction = 50

	defaultBaseURL = "https://www.googleapis.com/youtube/v3"

	// requestTimeout bounds one Data API call end to end. Chat sends are
	// perishable (the lane's MaxAge is 5s), so a hung call must fail fast
	// enough that the ack/nack decision still lands inside the message's
	// useful life.
	requestTimeout = 5 * time.Second

	maxErrorBody = 2048
)

// Client calls the YouTube Data API under a token source.
type Client struct {
	http   *http.Client
	base   string
	tokens TokenSource
}

// TokenSource yields a valid OAuth access token for one API call.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// TokenInvalidator is an optional TokenSource extension: a 401 asks the
// source to forget its cached token so the immediate retry mints a fresh one.
// Mirrors twitch.Source.Invalidate.
type TokenInvalidator interface {
	Invalidate()
}

// Typed errors. The worker maps each to drop (nil return) or nack (error).
var (
	// ErrQuotaExhausted means the project's daily quota is gone (403
	// quotaExceeded). Retrying today cannot succeed; the daily budget bucket
	// should have refused admission first, so this also signals drift between
	// our budget and Google's ledger.
	ErrQuotaExhausted = errors.New("youtube: daily quota exhausted")
	// ErrRateLimited is transient pressure (429 or reason rateLimitExceeded):
	// nack for paced redelivery.
	ErrRateLimited = errors.New("youtube: rate limited")
	// ErrChatEnded covers liveChatEnded / liveChatDisabled: the chat is gone
	// for good; redelivery can never succeed.
	ErrChatEnded = errors.New("youtube: live chat ended")
	// ErrChatNotFound (404 liveChatNotFound): stale liveChatId after a
	// broadcast restart. The next lifecycle event repopulates the directory;
	// this send is unrecoverable.
	ErrChatNotFound = errors.New("youtube: live chat not found")
	// ErrAuth means credentials were rejected even after one fresh-token
	// retry: a permanent authorization problem, dropped loudly like Twitch's.
	ErrAuth = errors.New("youtube: unauthorized")
)

// NewClient builds the Data API client against the production endpoint.
func NewClient(tokens TokenSource) *Client {
	return &Client{
		http:   &http.Client{Timeout: requestTimeout},
		base:   defaultBaseURL,
		tokens: tokens,
	}
}

// SetTransport swaps the HTTP transport (tests inject fakes here).
func (c *Client) SetTransport(rt http.RoundTripper) { c.http.Transport = rt }

// SendChatMessage inserts one text message into a live chat.
func (c *Client) SendChatMessage(ctx context.Context, liveChatID, text string) error {
	body := map[string]any{
		"snippet": map[string]any{
			"liveChatId":         liveChatID,
			"type":               "textMessageEvent",
			"textMessageDetails": map[string]any{"messageText": text},
		},
	}
	return c.do(ctx, http.MethodPost, "/liveChatMessages?part=snippet", body)
}

// DeleteChatMessage removes one chat message by id.
func (c *Client) DeleteChatMessage(ctx context.Context, msgID string) error {
	return c.do(ctx, http.MethodDelete,
		"/liveChatMessages?id="+url.QueryEscape(msgID), nil)
}

// Ban permanently bans a channel from a live chat.
func (c *Client) Ban(ctx context.Context, liveChatID, targetChannelID string) error {
	return c.ban(ctx, liveChatID, targetChannelID, "permanent", 0)
}

// Timeout temporarily bans a channel for durationSeconds.
func (c *Client) Timeout(ctx context.Context, liveChatID, targetChannelID string, durationSeconds int64) error {
	return c.ban(ctx, liveChatID, targetChannelID, "temporary", durationSeconds)
}

func (c *Client) ban(ctx context.Context, liveChatID, targetChannelID, kind string, durationSeconds int64) error {
	snippet := map[string]any{
		"liveChatId":        liveChatID,
		"type":              kind,
		"bannedUserDetails": map[string]any{"channelId": targetChannelID},
	}
	if durationSeconds > 0 {
		snippet["banDurationSeconds"] = durationSeconds
	}
	return c.do(ctx, http.MethodPost, "/liveChatBans?part=snippet", map[string]any{"snippet": snippet})
}

// do runs one classified API call. A 401 gets exactly one retry with a fresh
// token (an access token can expire mid-flight); a SECOND rejection with a
// fresh token is the permanent ErrAuth. Everything else classifies once.
func (c *Client) do(ctx context.Context, method, path string, body any) error {
	payload, err := codec.Marshal(body)
	if err != nil {
		return fmt.Errorf("youtube: encode body: %w", err)
	}

	for attempt := 0; ; attempt++ {
		token, terr := c.tokens.Token(ctx)
		if terr != nil {
			return fmt.Errorf("youtube: token source: %w", terr)
		}

		err = c.call(ctx, method, path, payload, token)
		if !errors.Is(err, errStaleToken) {
			return err
		}
		if attempt > 0 {
			// A fresh token was also refused: permanent authorization problem.
			return ErrAuth
		}
		if inv, ok := c.tokens.(TokenInvalidator); ok {
			inv.Invalidate()
		}
	}
}

// errStaleToken marks a rejection worth one fresh-token retry. It never
// escapes this package: the retry loop converts it to ErrAuth on the second
// round.
var errStaleToken = errors.New("youtube: stale token")

func (c *Client) call(ctx context.Context, method, path string, payload []byte, token string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return err // network/transient: caller nacks
	}
	defer drain(res)

	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil

	case res.StatusCode == http.StatusUnauthorized:
		return errStaleToken

	case res.StatusCode == http.StatusForbidden:
		reason := readBody(res)
		switch {
		case strings.Contains(reason, "quotaExceeded"):
			return ErrQuotaExhausted
		case strings.Contains(reason, "liveChatDisabled"), strings.Contains(reason, "liveChatEnded"):
			return ErrChatEnded
		default:
			return ErrAuth
		}

	case res.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited

	case res.StatusCode == http.StatusNotFound:
		return ErrChatNotFound

	case res.StatusCode >= 400:
		return fmt.Errorf("youtube: api rejected request (%d): %s",
			res.StatusCode, readBody(res))
	}

	return fmt.Errorf("youtube: unexpected api status %d", res.StatusCode)
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
