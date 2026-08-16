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
