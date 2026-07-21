package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

func (w *Worker) processAPI(ctx context.Context, payload *outgress.Message) error {
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	return w.execute(ctx, payload)
}

func (w *Worker) execute(ctx context.Context, payload *outgress.Message) error {
	res, err := w.executeRequest(ctx, payload)
	if err != nil {
		return err
	}
	defer drainResponse(res)

	return w.helixResult(ctx, payload, res)
}

// executeRequest performs one Twitch call and returns the still-open response.
// Most actions use execute, which consumes status and drains it immediately;
// compound actions such as /pin need to decode a successful response first.
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

// helixResult maps the Helix response status to the lane's ack/nack decision:
// 429 and 5xx nack for paced redelivery, everything 4xx is dropped (acked)
// because redelivering it can never succeed.
func (w *Worker) helixResult(ctx context.Context, payload *outgress.Message, res *http.Response) error {
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		w.log.Warn("twitch rate limited the app",
			zap.String("endpoint", payload.Endpoint),
			zap.Duration("retry_after", twitch.RetryAfter(res)))
		return fmt.Errorf("twitch 429 on %s", payload.Endpoint)

	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		w.dropAuthFailure(ctx, payload, res)
		return nil

	case res.StatusCode >= 500:
		return fmt.Errorf("twitch server error: %d", res.StatusCode)

	case res.StatusCode >= 400:
		w.dropRejected(ctx, payload, res)
		return nil
	}

	return nil
}

// dropAuthFailure handles a 401/403: the client already retried once with a
// freshly minted token and Twitch still rejected it. A fresh token being
// refused is a PERMANENT authorization problem (a missing scope, the bot not
// being a moderator of the channel, or a moderator_id/token mismatch), not a
// recoverable token expiry, so redelivering it just loops forever and poisons
// the lane. Drop it (ack) and surface it loudly + to New Relic for a human to
// fix (re-auth / mod the bot). Twitch's body states which of the three.
func (w *Worker) dropAuthFailure(ctx context.Context, payload *outgress.Message, res *http.Response) {
	body := readErrorBody(res)
	w.log.Error("dropping request: twitch rejected our credentials (permanent authz problem, not retryable)",
		zap.Int("status", res.StatusCode),
		zap.String("endpoint", payload.Endpoint),
		zap.String("as", payload.As),
		zap.String("body", body))
	noticeError(ctx, fmt.Errorf("twitch auth failure: %d %s", res.StatusCode, body))
}

func (w *Worker) dropRejected(ctx context.Context, payload *outgress.Message, res *http.Response) {
	body := readErrorBody(res)
	w.log.Error("dropping request twitch rejected",
		zap.Int("status", res.StatusCode),
		zap.String("endpoint", payload.Endpoint),
		zap.String("body", body))
	noticeError(ctx, fmt.Errorf("twitch rejected request: %d %s", res.StatusCode, body))
}

// readErrorBody captures enough of a rejection body for the log line without
// letting an unbounded response pin the worker.
func readErrorBody(res *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return string(body)
}

const maxResponseDrain = 64 << 10

// drainResponse makes small HTTP/1.1 responses reusable without allowing an
// unexpectedly large or non-terminating body to pin a worker indefinitely. The
// client's total timeout still bounds a slow body.
func drainResponse(res *http.Response) {
	_, _ = io.CopyN(io.Discard, res.Body, maxResponseDrain+1)
	_ = res.Body.Close()
}
