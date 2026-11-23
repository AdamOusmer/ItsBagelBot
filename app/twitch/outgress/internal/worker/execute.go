// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

type helixOutcome uint8

const (
	helixUnconfirmed helixOutcome = iota
	helixSucceeded
	helixRejected
)

// twitchRateLimited reports a confirmed 429 from Twitch itself, as opposed to
// our own pre-send bucket refusal. It is expected and retryable, so it nacks
// quietly (like errRateLimitFirst/errRateLimitShared) instead of counting
// toward the error rate, and it waits out Twitch's own Ratelimit-Reset
// instead of redelivering immediately into the same limit.
type twitchRateLimited struct {
	endpoint   string
	retryAfter time.Duration
}

func (e twitchRateLimited) Error() string            { return fmt.Sprintf("twitch 429 on %s", e.endpoint) }
func (e twitchRateLimited) ExpectedNack() bool        { return true }
func (e twitchRateLimited) RetryAfter() time.Duration { return e.retryAfter }

func (w *Worker) processAPI(ctx context.Context, payload *outgress.Message) error {
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	return w.execute(ctx, payload)
}

func (w *Worker) execute(ctx context.Context, payload *outgress.Message) error {
	_, err := w.executeHelix(ctx, payload)
	return err
}

func (w *Worker) executeHelix(ctx context.Context, payload *outgress.Message) (helixOutcome, error) {
	res, err := w.executeRequest(ctx, payload)
	if err != nil {
		return helixUnconfirmed, err
	}
	defer drainResponse(res)

	outcome, err := w.helixResult(ctx, payload, res)
	if err != nil {
		return outcome, err
	}
	w.recordChatResult(ctx, payload, res)
	return outcome, nil
}

func (w *Worker) executeRequest(ctx context.Context, payload *outgress.Message) (*http.Response, error) {
	started := time.Now()
	defer recordStageDuration(ctx, "outgress.twitch_ms", started)

	res, err := w.callTwitch(ctx, twitch.ParseIdentity(payload.As), payload.BroadcasterID,
		twitch.HelixCall{Method: payload.Method, Endpoint: payload.Endpoint, Body: payload.Payload})
	if err != nil {
		w.log.Error("twitch request failed", zap.Error(err))
		return nil, err
	}
	return res, nil
}

func (w *Worker) helixResult(ctx context.Context, payload *outgress.Message, res *http.Response) (helixOutcome, error) {
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		retryAfter := twitch.RetryAfter(res)
		w.log.Warn("twitch rate limited the app",
			zap.String("endpoint", payload.Endpoint),
			zap.Duration("retry_after", retryAfter))
		return helixUnconfirmed, twitchRateLimited{endpoint: payload.Endpoint, retryAfter: retryAfter}

	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		w.dropAuthFailure(ctx, payload, res)
		return helixRejected, nil

	case res.StatusCode >= 500:
		return helixUnconfirmed, fmt.Errorf("twitch server error: %d", res.StatusCode)

	case res.StatusCode >= 400:
		w.dropRejected(ctx, payload, res)
		return helixRejected, nil

	case res.StatusCode >= 200 && res.StatusCode < 300:
		return helixSucceeded, nil
	}

	return helixUnconfirmed, nil
}

func (w *Worker) dropAuthFailure(ctx context.Context, payload *outgress.Message, res *http.Response) {
	w.analytics.Count(catalog.OutgressDropped, "helix", "auth")
	body := readErrorBody(res)
	w.log.Error("dropping request: twitch rejected our credentials (permanent authz problem, not retryable)",
		zap.Int("status", res.StatusCode),
		zap.String("endpoint", payload.Endpoint),
		zap.String("as", payload.As),
		zap.String("broadcaster_id", payload.BroadcasterID),
		zap.String("body", body))
	noticeError(ctx, fmt.Errorf("twitch auth failure: %d %s", res.StatusCode, body))
}

func (w *Worker) dropRejected(ctx context.Context, payload *outgress.Message, res *http.Response) {
	w.analytics.Count(catalog.OutgressDropped, "helix", "rejected")
	body := readErrorBody(res)
	w.log.Error("dropping request twitch rejected",
		zap.Int("status", res.StatusCode),
		zap.String("endpoint", payload.Endpoint),
		zap.String("body", body))
	noticeError(ctx, fmt.Errorf("twitch rejected request: %d %s", res.StatusCode, body))
}

func readErrorBody(res *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return string(body)
}

const maxResponseDrain = 64 << 10

func drainResponse(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxResponseDrain+1)
	_ = res.Body.Close()
}
