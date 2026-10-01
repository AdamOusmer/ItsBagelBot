// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"

	"github.com/stretchr/testify/assert"
)

func TestFriendlyUpstreamMapsStatusesToChatMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		wantMsg string
		wantPin core.Pin
	}{
		{"a local bucket denial retries on the next request", &core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true},
			"stats commands are busy right now, try again in a few seconds", core.PinNone},
		{"an upstream throttle backs off briefly", &core.UpstreamError{Status: 429},
			"stats provider is rate limiting us, try again in a minute", core.PinThrottle},
		{"a missing player is cached as absent", &core.UpstreamError{Status: 404},
			"player not found", core.PinNegative},
		{"an upstream message on a bad request is passed through", &core.UpstreamError{Status: 400, Message: "bad name"},
			"bad name", core.PinNegative},
		{"a refusal is told but never cached", &core.UpstreamError{Status: 403},
			"stats lookup not permitted right now", core.PinNone},
		{"infrastructure failures propagate instead of chatting", errors.New("dial tcp: timeout"), "", core.PinNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg, pin := core.FriendlyUpstream(tc.err)
			assert.Equal(t, tc.wantMsg, msg)
			assert.Equal(t, tc.wantPin, pin)
		})
	}
}

type replyOutcome struct {
	Body     string
	TTL      time.Duration
	Friendly *core.UpstreamError
	Err      error
}

type mapping struct {
	msg string
	pin core.Pin
}

func mapperFor(m *mapping) func(error) (string, core.Pin) {
	if m == nil {
		return core.FriendlyUpstream
	}
	return func(error) (string, core.Pin) { return m.msg, m.pin }
}

func TestBuildReplyShapesTheCachedAnswer(t *testing.T) {
	const ttl, negativeTTL = time.Minute, 15 * time.Second
	notFound := &core.UpstreamError{Status: 404}
	throttled := &core.UpstreamError{Status: 429}
	localDeny := &core.UpstreamError{Status: 429, LocalDeny: true}
	longRetry := &core.UpstreamError{Status: 429, RetryAfter: 90 * time.Second}
	shortRetry := &core.UpstreamError{Status: 429, RetryAfter: 5 * time.Second}
	refusedWithRetry := &core.UpstreamError{Status: 403, RetryAfter: 10 * time.Minute}
	absentWithRetry := &core.UpstreamError{Status: 404, RetryAfter: 10 * time.Minute}
	serverError := &core.UpstreamError{Status: 500}
	dialFailure := errors.New("dial tcp: timeout")

	for _, tc := range []struct {
		name   string
		fetch  error
		mapper *mapping
		want   replyOutcome
	}{
		{"caches a success for the fresh window", nil, nil,
			replyOutcome{Body: `{"ok":"1"}`, TTL: ttl}},
		{"caches an absence for the negative window", notFound, nil,
			replyOutcome{Body: `{"error":"player not found"}`, TTL: negativeTTL, Friendly: notFound}},
		{"pins an upstream throttle briefly", throttled, nil,
			replyOutcome{Body: `{"error":"stats provider is rate limiting us, try again in a minute"}`, TTL: core.ThrottleTTL, Friendly: throttled}},
		{"never caches a local bucket denial", localDeny, nil,
			replyOutcome{Body: `{"error":"stats commands are busy right now, try again in a few seconds"}`, Friendly: localDeny}},
		{"TestBuildReplyHonorsRetryAfter", longRetry, nil,
			replyOutcome{Body: `{"error":"stats provider is rate limiting us, try again in a minute"}`, TTL: 90 * time.Second, Friendly: longRetry}},
		{"TestBuildReplyHonorsRetryAfter: ThrottleTTL is the floor when Retry-After is shorter", shortRetry, nil,
			replyOutcome{Body: `{"error":"stats provider is rate limiting us, try again in a minute"}`, TTL: core.ThrottleTTL, Friendly: shortRetry}},
		{"TestBuildReplyDoesNotExtendNonThrottlePins: an uncached refusal ignores Retry-After", refusedWithRetry, &mapping{"not permitted", core.PinNone},
			replyOutcome{Body: `{"error":"not permitted"}`, Friendly: refusedWithRetry}},
		{"TestBuildReplyDoesNotExtendNonThrottlePins: an absence ignores Retry-After", absentWithRetry, &mapping{"not found", core.PinNegative},
			replyOutcome{Body: `{"error":"not found"}`, TTL: negativeTTL, Friendly: absentWithRetry}},
		{"TestBuildReplyWithMapper", serverError, &mapping{"custom friendly error", core.PinThrottle},
			replyOutcome{Body: `{"error":"custom friendly error"}`, TTL: core.ThrottleTTL, Friendly: serverError}},
		{"propagates an unmapped infrastructure failure", dialFailure, nil,
			replyOutcome{Err: dialFailure}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, gotTTL, friendly, err := core.BuildReplyWithMapper(context.Background(), ttl, negativeTTL,
				func(context.Context) (any, error) {
					if tc.fetch != nil {
						return nil, tc.fetch
					}
					return map[string]string{"ok": "1"}, nil
				},
				func(msg string) any { return map[string]string{"error": msg} },
				mapperFor(tc.mapper))

			assert.Equal(t, tc.want, replyOutcome{Body: string(body), TTL: gotTTL, Friendly: friendly, Err: err})
		})
	}
}
