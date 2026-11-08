// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type telemetryKey struct{}

type requestTelemetry struct {
	provider string
	endpoint string
	mu       sync.Mutex
	failure  string
}

// WithRequestTelemetry accepts registered provider and endpoint names, never request data.
func WithRequestTelemetry(ctx context.Context, provider, endpoint string) context.Context {
	return context.WithValue(ctx, telemetryKey{}, &requestTelemetry{provider: provider, endpoint: endpoint})
}

// DetachedRequestTelemetry keeps only registered names for background cache logs.
// Request cancellation, trace state and captured fetch errors are not retained.
func DetachedRequestTelemetry(parent context.Context) context.Context {
	ctx := context.Background()
	if t, ok := parent.Value(telemetryKey{}).(*requestTelemetry); ok {
		return WithRequestTelemetry(ctx, t.provider, t.endpoint)
	}
	return ctx
}

// RecordFetchError retains only a bounded class. A successful fallback still has an ok outcome.
func RecordFetchError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if t, ok := ctx.Value(telemetryKey{}).(*requestTelemetry); ok {
		t.mu.Lock()
		defer t.mu.Unlock()
		outcome := fetchErrorOutcome(err)
		if failurePriority(outcome) > failurePriority(t.failure) {
			t.failure = outcome
		}
	}
}

func fetchErrorOutcome(err error) string {
	var timeout net.Error
	switch {
	case errors.Is(err, errSharedFetchPanic):
		return "internal_error"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return upstreamOutcome(upstream)
	}
	return "upstream_error"
}

func upstreamOutcome(err *UpstreamError) string {
	switch err.Status {
	case 400:
		return "invalid"
	case 404:
		return "not_found"
	case 401, 403:
		return "denied"
	case 429:
		if err.LocalDeny {
			return "local_limited"
		}
		return "upstream_limited"
	default:
		return "upstream_error"
	}
}

func failurePriority(outcome string) int {
	switch outcome {
	case "timeout", "upstream_error", "internal_error":
		return 3
	case "":
		return 0
	case "canceled":
		return 1
	default:
		return 2
	}
}

// RecordCacheOutcome exposes flags so nested caches cannot hide an earlier miss.
// The supported outcomes are hit, stale, miss, refresh and fresh.
func RecordCacheOutcome(ctx context.Context, outcome string) {
	switch outcome {
	case "hit", "stale", "miss", "refresh", "fresh":
	default:
		outcome = "other"
	}
	newrelic.FromContext(ctx).AddAttribute("gossip.cache."+outcome, true)
}

// RecordCacheError also logs detached refresh failures; neither keys nor raw errors escape.
func RecordCacheError(ctx context.Context, operation string, err error) {
	if err == nil {
		return
	}
	if expectedRefreshResult(operation, err) {
		return
	}
	switch operation {
	case "read", "write", "delete", "claim", "refresh":
	default:
		operation = "other"
	}
	outcome := fetchErrorOutcome(err)
	newrelic.FromContext(ctx).AddAttribute("gossip.cache.error", operation)
	fields := []zap.Field{zap.String("operation", operation), zap.String("error_class", outcome)}
	if t, ok := ctx.Value(telemetryKey{}).(*requestTelemetry); ok {
		fields = append(fields, zap.String("provider", t.provider), zap.String("endpoint", t.endpoint))
	}
	zap.L().Warn("gossip cache operation failed", fields...)
}

func expectedRefreshResult(operation string, err error) bool {
	if operation != "refresh" {
		return false
	}
	var upstream *UpstreamError
	if !errors.As(err, &upstream) {
		return false
	}
	switch upstream.Status {
	case 400, 404:
		return true
	default:
		return false
	}
}

type replyStatus struct {
	Status gossiprpc.FetchStatus `json:"status"`
	Error  string                `json:"error"`
}

// ReplyOutcome classifies the encoded reply without exporting its error text or payload.
func ReplyOutcome(ctx context.Context, body []byte) string {
	if err := ctx.Err(); err != nil {
		return fetchErrorOutcome(err)
	}
	var reply replyStatus
	if err := codec.Unmarshal(body, &reply); err != nil {
		return "internal_error"
	}
	return reply.outcome(ctx)
}

func (reply replyStatus) successful() bool {
	if reply.Error != "" {
		return false
	}
	return reply.Status == "" || reply.Status == gossiprpc.FetchOK
}

func (reply replyStatus) outcome(ctx context.Context) string {
	if reply.successful() {
		return "ok"
	}
	if failure := capturedFailure(ctx); failure != "" {
		return failure
	}
	if reply.Status != "" {
		return customOutcome(reply.Status)
	}
	return replyErrorOutcome(reply.Error)
}

func capturedFailure(ctx context.Context) string {
	t, ok := ctx.Value(telemetryKey{}).(*requestTelemetry)
	if !ok {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failure
}

func customOutcome(status gossiprpc.FetchStatus) string {
	switch status {
	case gossiprpc.FetchDenied:
		return "denied"
	case gossiprpc.FetchBadDef:
		return "invalid"
	case gossiprpc.FetchLimited:
		return "limited"
	case gossiprpc.FetchTimeout:
		return "timeout"
	default:
		return "upstream_error"
	}
}

func replyErrorOutcome(message string) string {
	message = strings.ToLower(message)
	switch {
	case strings.HasPrefix(message, "missing "), strings.HasPrefix(message, "invalid "),
		strings.HasPrefix(message, "unknown "), strings.HasPrefix(message, "only "), message == "bad request":
		return "invalid"
	case strings.Contains(message, "not found"), strings.HasPrefix(message, "no stats for "):
		return "not_found"
	case message == "stats lookup not permitted right now", message == "spotify playback not permitted right now":
		return "denied"
	case message == "stats commands are busy right now, try again in a few seconds",
		message == "spotify is busy right now, try again in a few seconds":
		return "local_limited"
	case message == "stats provider is rate limiting us, try again in a minute",
		message == "spotify is rate limiting requests right now, try again in a moment",
		message == "govee is rate limiting scene lookups, try again in a minute":
		return "upstream_limited"
	case message == "internal error":
		return "internal_error"
	case strings.HasSuffix(message, " lookup failed"), message == "lookup failed",
		strings.HasPrefix(message, "could not reach "), strings.HasPrefix(message, "could not read "),
		message == "snapshot store failed", message == "snapshot delete failed",
		message == "spotify is unavailable right now, try again in a moment":
		return "upstream_error"
	default:
		// Cached 400/404 replies may contain arbitrary provider text without the original
		// status. Treat them as refusals rather than inventing an infrastructure error.
		return "refused"
	}
}
