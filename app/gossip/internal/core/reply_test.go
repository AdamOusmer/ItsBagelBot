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
	"github.com/stretchr/testify/require"
)

func TestFriendlyUpstream429Origins(t *testing.T) {
	msg, pin := core.FriendlyUpstream(&core.UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true})
	assert.Equal(t, "stats commands are busy right now, try again in a few seconds", msg)
	assert.Equal(t, core.PinNone, pin, "a bucket denial must retry on the next request")

	msg, pin = core.FriendlyUpstream(&core.UpstreamError{Status: 429})
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", msg)
	assert.Equal(t, core.PinThrottle, pin, "an upstream throttle must back off briefly")
}

func TestFriendlyUpstreamClasses(t *testing.T) {
	msg, pin := core.FriendlyUpstream(&core.UpstreamError{Status: 404})
	assert.Equal(t, "player not found", msg)
	assert.Equal(t, core.PinNegative, pin)

	msg, pin = core.FriendlyUpstream(&core.UpstreamError{Status: 403})
	assert.Equal(t, "stats lookup not permitted right now", msg)
	assert.Equal(t, core.PinNone, pin)

	msg, _ = core.FriendlyUpstream(errors.New("dial tcp: timeout"))
	assert.Empty(t, msg, "infrastructure failures must propagate, not chat")
}

func TestBuildReplyPinTTLs(t *testing.T) {
	const negativeTTL = 5 * time.Minute
	errReply := func(msg string) any { return map[string]string{"error": msg} }
	build := func(err error) (time.Duration, *core.UpstreamError) {
		t.Helper()
		b, ttl, friendly, berr := core.BuildReply(context.Background(), time.Minute, negativeTTL,
			func(context.Context) (any, error) { return nil, err }, errReply)
		require.NoError(t, berr)
		require.NotEmpty(t, b)
		return ttl, friendly
	}

	ttl, friendly := build(&core.UpstreamError{Status: 404})
	assert.Equal(t, negativeTTL, ttl)
	require.NotNil(t, friendly)

	ttl, friendly = build(&core.UpstreamError{Status: 429})
	assert.Equal(t, core.ThrottleTTL, ttl)
	require.NotNil(t, friendly)
	assert.False(t, friendly.LocalDeny)

	ttl, friendly = build(&core.UpstreamError{Status: 429, LocalDeny: true})
	assert.Equal(t, time.Duration(0), ttl)
	require.NotNil(t, friendly)
	assert.True(t, friendly.LocalDeny)
}

func TestBuildReplySuccess(t *testing.T) {
	b, ttl, friendly, err := core.BuildReply(context.Background(), time.Minute, time.Hour,
		func(context.Context) (any, error) { return map[string]string{"ok": "1"}, nil },
		func(msg string) any { return map[string]string{"error": msg} })
	require.NoError(t, err)
	assert.NotEmpty(t, b)
	assert.Equal(t, time.Minute, ttl)
	assert.Nil(t, friendly)
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
	badName := &core.UpstreamError{Status: 400, Message: "bad name"}
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
		{"passes an upstream message through and caches it as an absence", badName, nil,
			replyOutcome{Body: `{"error":"bad name"}`, TTL: negativeTTL, Friendly: badName}},
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
