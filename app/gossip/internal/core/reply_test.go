// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two 429 origins must stay distinguishable in chat: a bucket denial
// refills in seconds and cost no upstream call, an upstream 429 means the
// provider is throttling our address. Collapsing them to one message is what
// made the real incident undiagnosable from the outside.
func TestFriendlyUpstream429Origins(t *testing.T) {
	msg, pin := FriendlyUpstream(&UpstreamError{Status: 429, Message: "standard rate limit exceeded", LocalDeny: true})
	assert.Equal(t, "stats commands are busy right now, try again in a few seconds", msg)
	assert.Equal(t, PinNone, pin, "a bucket denial must retry on the next request")

	msg, pin = FriendlyUpstream(&UpstreamError{Status: 429})
	assert.Equal(t, "stats provider is rate limiting us, try again in a minute", msg)
	assert.Equal(t, PinThrottle, pin, "an upstream throttle must back off briefly")
}

func TestFriendlyUpstreamClasses(t *testing.T) {
	msg, pin := FriendlyUpstream(&UpstreamError{Status: 404})
	assert.Equal(t, "player not found", msg)
	assert.Equal(t, PinNegative, pin)

	msg, pin = FriendlyUpstream(&UpstreamError{Status: 403})
	assert.Equal(t, "stats lookup not permitted right now", msg)
	assert.Equal(t, PinNone, pin)

	msg, _ = FriendlyUpstream(errors.New("dial tcp: timeout"))
	assert.Empty(t, msg, "infrastructure failures must propagate, not chat")
}

// BuildReply maps each Pin class onto its store TTL: negatives keep the
// endpoint's negativeTTL, upstream throttles pin for ThrottleTTL, and bucket
// denials answer with TTL zero so the very next request retries.
func TestBuildReplyPinTTLs(t *testing.T) {
	const negativeTTL = 5 * time.Minute
	errReply := func(msg string) any { return map[string]string{"error": msg} }
	build := func(err error) (time.Duration, *UpstreamError) {
		t.Helper()
		b, ttl, friendly, berr := BuildReply(context.Background(), time.Minute, negativeTTL,
			func(context.Context) (any, error) { return nil, err }, errReply)
		require.NoError(t, berr)
		require.NotEmpty(t, b)
		return ttl, friendly
	}

	ttl, friendly := build(&UpstreamError{Status: 404})
	assert.Equal(t, negativeTTL, ttl)
	require.NotNil(t, friendly)

	ttl, friendly = build(&UpstreamError{Status: 429})
	assert.Equal(t, ThrottleTTL, ttl)
	require.NotNil(t, friendly)
	assert.False(t, friendly.LocalDeny)

	ttl, friendly = build(&UpstreamError{Status: 429, LocalDeny: true})
	assert.Equal(t, time.Duration(0), ttl)
	require.NotNil(t, friendly)
	assert.True(t, friendly.LocalDeny)
}

// A success reports no friendly failure and keeps the endpoint's TTL.
func TestBuildReplySuccess(t *testing.T) {
	b, ttl, friendly, err := BuildReply(context.Background(), time.Minute, time.Hour,
		func(context.Context) (any, error) { return map[string]string{"ok": "1"}, nil },
		func(msg string) any { return map[string]string{"error": msg} })
	require.NoError(t, err)
	assert.NotEmpty(t, b)
	assert.Equal(t, time.Minute, ttl)
	assert.Nil(t, friendly)
}

func TestBuildReplyHonorsRetryAfter(t *testing.T) {
	const negativeTTL = 5 * time.Minute
	errReply := func(msg string) any { return map[string]string{"error": msg} }

	// An upstream 429 with a Retry-After larger than ThrottleTTL (20s) must extend the pin TTL.
	b, ttl, friendly, err := BuildReply(context.Background(), time.Minute, negativeTTL,
		func(context.Context) (any, error) {
			return nil, &UpstreamError{Status: 429, RetryAfter: 90 * time.Second}
		}, errReply)
	require.NoError(t, err)
	assert.NotEmpty(t, b)
	assert.Equal(t, 90*time.Second, ttl, "Retry-After must override ThrottleTTL when larger")
	require.NotNil(t, friendly)
	assert.Equal(t, 90*time.Second, friendly.RetryAfter)

	// A Retry-After shorter than ThrottleTTL keeps ThrottleTTL as floor.
	_, ttlShort, _, _ := BuildReply(context.Background(), time.Minute, negativeTTL,
		func(context.Context) (any, error) {
			return nil, &UpstreamError{Status: 429, RetryAfter: 5 * time.Second}
		}, errReply)
	assert.Equal(t, ThrottleTTL, ttlShort, "ThrottleTTL is the floor when Retry-After is shorter")
}

// Retry-After is a throttle signal and may only stretch a throttle pin.
// PinNone exists to heal on the next request (our own bucket, a key permission
// fixed out of band); pinning one on a stray header caches the failure past the
// moment it was fixed.
func TestBuildReplyDoesNotExtendNonThrottlePins(t *testing.T) {
	errReply := func(msg string) any { return map[string]string{"error": msg} }
	const negativeTTL = 15 * time.Second

	_, ttlNone, _, err := BuildReplyWithMapper(context.Background(), time.Minute, negativeTTL,
		func(context.Context) (any, error) {
			return nil, &UpstreamError{Status: 403, RetryAfter: 10 * time.Minute}
		}, errReply, func(error) (string, Pin) { return "not permitted", PinNone })
	require.NoError(t, err)
	assert.Zero(t, ttlNone, "a PinNone failure must not be cached, Retry-After or not")

	_, ttlNegative, _, err := BuildReplyWithMapper(context.Background(), time.Minute, negativeTTL,
		func(context.Context) (any, error) {
			return nil, &UpstreamError{Status: 404, RetryAfter: 10 * time.Minute}
		}, errReply, func(error) (string, Pin) { return "not found", PinNegative })
	require.NoError(t, err)
	assert.Equal(t, negativeTTL, ttlNegative, "a rate-limit header says nothing about how long an absence lasts")
}

func TestBuildReplyWithMapper(t *testing.T) {
	customMapper := func(err error) (string, Pin) {
		return "custom friendly error", PinThrottle
	}
	errReply := func(msg string) any { return map[string]string{"error": msg} }
	b, ttl, _, err := BuildReplyWithMapper(context.Background(), time.Minute, 5*time.Minute,
		func(context.Context) (any, error) {
			return nil, &UpstreamError{Status: 500}
		}, errReply, customMapper)
	require.NoError(t, err)
	assert.NotEmpty(t, b)
	assert.Equal(t, ThrottleTTL, ttl)
	assert.Contains(t, string(b), "custom friendly error")
}
