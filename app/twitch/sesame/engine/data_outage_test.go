// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"github.com/stretchr/testify/require"
)

func TestDataOutageKeepsRepliesFlowingPastEveryFetchBatch(t *testing.T) {
	t.Setenv("NATS_PULL_FETCH_BATCH", "1")
	b := newCompletionBroker(t)
	b.pub.failAll.Store(true)
	defer b.pub.failAll.Store(false)
	p, reporter := b.pipeline(t, valkeytest.Spawn(t))
	t.Cleanup(reporter.Close)
	t.Cleanup(p.Close)
	for i := range 12 {
		msg := envelopeMsg(t, fmt.Sprint(i), map[string]any{"lane": "premium", "msg_id": fmt.Sprint(i), "chatter_user_id": "999", "text": "!hello"})
		_, err := b.js.Publish(completionSubject, msg.Payload)
		require.NoError(t, err)
	}
	started := time.Now()
	var pending []*bus.Message
	for range 12 {
		select {
		case msg := <-b.messages:
			require.NoError(t, p.Process(msg))
			assertPendingCompletion(t, msg)
			pending = append(pending, msg)
		case <-time.After(time.Second):
			t.Fatal("DATA outage stopped chat processing")
		}
	}
	info, err := b.js.StreamInfo(bus.OutgressStream.Name)
	require.NoError(t, err)
	require.EqualValues(t, 12, info.State.Msgs)
	require.Less(t, time.Since(started), time.Second)
	b.pub.failAll.Store(false)
	p.Finish(t.Context())
	for _, msg := range pending {
		awaitCompletion(t, msg, true)
	}
}
