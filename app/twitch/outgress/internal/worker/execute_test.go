// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatSendOn429WaitsOutTwitchsResetInsteadOfAnImmediateRetry(t *testing.T) {
	reset := time.Now().Add(5 * time.Second)
	rt := &scriptedTransport{responses: []scriptedResponse{{
		status:  http.StatusTooManyRequests,
		headers: http.Header{"Ratelimit-Reset": []string{strconv.FormatInt(reset.Unix(), 10)}},
	}}}

	err := testMessage{Type: "chat", Payload: `{"message":"hello"}`}.send(pipelineWorker(t, rt))

	require.Error(t, err)
	assert.Equal(t, "twitch 429 on /helix/chat/messages", err.Error())

	var nack interface{ ExpectedNack() bool }
	require.True(t, errors.As(err, &nack), "429 must mark itself an expected nack, not a real error")
	assert.True(t, nack.ExpectedNack())

	var retryable interface{ RetryAfter() time.Duration }
	require.True(t, errors.As(err, &retryable), "429 must carry Twitch's own backoff")
	assert.InDelta(t, 5*time.Second, retryable.RetryAfter(), float64(time.Second))
}
