// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestReplyOutcomeDoesNotExportProviderMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		err  error
		want string
	}{
		{"success", `{"player":"private-account"}`, nil, "ok"},
		{"successful fallback", `{"window":"lifetime"}`, errors.New("secret upstream URL"), "ok"},
		{"upstream absence", `{"error":"arbitrary upstream text"}`, &UpstreamError{Status: 404}, "not_found"},
		{"cached absence", `{"error":"player not found"}`, nil, "not_found"},
		{"invalid request", `{"error":"missing account or channel"}`, nil, "invalid"},
		{"private stats", `{"error":"stats lookup not permitted right now"}`, nil, "denied"},
		{"local budget", `{"error":"busy"}`, &UpstreamError{Status: 429, LocalDeny: true}, "local_limited"},
		{"upstream budget", `{"error":"busy"}`, &UpstreamError{Status: 429}, "upstream_limited"},
		{"cached upstream budget", `{"error":"stats provider is rate limiting us, try again in a minute"}`, nil, "upstream_limited"},
		{"cached Spotify budget", `{"error":"Spotify is rate limiting requests right now, try again in a moment"}`, nil, "upstream_limited"},
		{"local Spotify budget", `{"error":"Spotify is busy right now, try again in a few seconds"}`, nil, "local_limited"},
		{"HTTP timeout fallback", `{"error":"stats lookup failed"}`, context.DeadlineExceeded, "timeout"},
		{"custom timeout", `{"status":"timeout"}`, nil, "timeout"},
		{"custom denial", `{"status":"denied"}`, nil, "denied"},
		{"custom invalid definition", `{"status":"bad_def"}`, nil, "invalid"},
		{"custom limited", `{"status":"limited"}`, nil, "limited"},
		{"unrecognized status", `{"status":"sensitive URL"}`, nil, "upstream_error"},
		{"unclassified cached refusal", `{"error":"secret-access-token"}`, nil, "refused"},
		{"known fallback", `{"error":"stats lookup failed"}`, nil, "upstream_error"},
		{"marshal failure", `{"error":"internal error"}`, nil, "internal_error"},
		{"malformed reply", `not json`, nil, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithRequestTelemetry(context.Background(), "fortnite", "stats")
			RecordFetchError(ctx, tc.err)
			assert.Equal(t, tc.want, ReplyOutcome(ctx, []byte(tc.body)))
		})
	}
}

func TestDetachedTelemetryRetainsOnlyRegisteredNames(t *testing.T) {
	type secretKey struct{}
	parent := WithRequestTelemetry(context.WithValue(context.Background(), secretKey{}, "sensitive context"), "fortnite", "stats")
	RecordFetchError(parent, context.DeadlineExceeded)
	parent, cancel := context.WithCancel(parent)
	ctx := DetachedRequestTelemetry(parent)
	cancel()
	assert.NoError(t, ctx.Err())
	assert.Nil(t, ctx.Value(secretKey{}))
	assert.Equal(t, "refused", ReplyOutcome(ctx, []byte(`{"error":"arbitrary cached text"}`)))
	telemetry, ok := ctx.Value(telemetryKey{}).(*requestTelemetry)
	require.True(t, ok)
	assert.Equal(t, "fortnite", telemetry.provider)
	assert.Equal(t, "stats", telemetry.endpoint)
}

func TestReplyOutcomeHonorsCancellationAndParallelFailures(t *testing.T) {
	ctx := WithRequestTelemetry(context.Background(), "paceman", "session")
	var wg sync.WaitGroup
	for _, err := range []error{&UpstreamError{Status: 404}, context.DeadlineExceeded} {
		wg.Go(func() { RecordFetchError(ctx, err) })
	}
	wg.Wait()
	assert.Equal(t, "timeout", ReplyOutcome(ctx, []byte(`{"error":"lookup failed"}`)))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	assert.Equal(t, "canceled", ReplyOutcome(canceled, []byte(`{"ok":true}`)))
	ctx = WithRequestTelemetry(context.Background(), "paceman", "session")
	RecordFetchError(ctx, &UpstreamError{Status: 404})
	RecordFetchError(ctx, context.Canceled)
	assert.Equal(t, "not_found", ReplyOutcome(ctx, []byte(`{"error":"player not found"}`)), "sibling cancellation must not hide the actual rejection")
}

func TestCacheFailureLogsAreUsefulWithoutKeysOrSecrets(t *testing.T) {
	observed, logs := observer.New(zap.WarnLevel)
	restore := zap.ReplaceGlobals(zap.New(observed))
	t.Cleanup(restore)
	ctx := WithRequestTelemetry(context.Background(), "fortnite", "stats")
	RecordCacheError(ctx, "write", errors.New("gossip:token:private-channel secret-access-token"))
	RecordCacheError(context.Background(), "untrusted-key", context.DeadlineExceeded)
	RecordCacheError(ctx, "read", nil)
	RecordCacheError(ctx, "refresh", &UpstreamError{Status: 404, Message: "player not found"})
	require.Len(t, logs.All(), 2)
	assert.Equal(t, map[string]any{
		"operation": "write", "error_class": "upstream_error", "provider": "fortnite", "endpoint": "stats",
	}, logs.All()[0].ContextMap())
	assert.Equal(t, map[string]any{"operation": "other", "error_class": "timeout"}, logs.All()[1].ContextMap())
	for _, outcome := range []string{"hit", "stale", "miss", "refresh", "fresh", "untrusted-key"} {
		assert.NotPanics(t, func() { RecordCacheOutcome(context.Background(), outcome) })
	}
}
