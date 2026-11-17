// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestPullExpiryDropsLateResolutionAndCountsStuckCompletion(t *testing.T) {
	for _, ack := range []bool{true, false} {
		t.Run(map[bool]string{true: "ACK", false: "NAK"}[ack], func(t *testing.T) {
			f := completionFixture(t)
			t.Setenv("NATS_PULL_ACK_WAIT", "300ms")
			trace := progressTrace(t, f)
			core, logs := observer.New(zap.WarnLevel)
			sub, err := NewSubscriber(f.url, "worker", zap.New(core))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sub.Close()) })
			messages := subscribe(t, sub, hotLane)
			f.appendMessage(t, hotLane, "stuck")
			original := receive(t, messages)
			reply := awaitPullAck(t, trace, ackFilter{progress: true}).Subject
			copy := receive(t, messages)
			require.Equal(t, original.UUID, copy.UUID)
			require.False(t, SubscriberHealthy(sub))
			expired := logs.FilterMessage("pull delivery progress expired; releasing for redelivery").All()
			require.Len(t, expired, 1)
			assert.EqualValues(t, 1, expired[0].ContextMap()["expired"])
			resolveTestMessage(original, ack)
			assertNoResolution(t, trace, reply)
			info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, PullLaneConsumerConfig("worker", hotLane).Name)
			require.NoError(t, err)
			assert.EqualValues(t, 1, info.NumAckPending)
			copy.Ack()
			require.Eventually(t, func() bool { return f.messageCount(TwitchIngressPremiumStream) == 0 }, time.Second, time.Millisecond)
		})
	}
}

func TestExpiryTelemetryEmitsWithoutCompletedWork(t *testing.T) {
	stats := &laneStats{destination: hotLane, sampleRate: 1}
	stats.expired.Add(3)
	params, ok := stats.drain()
	require.True(t, ok)
	assert.Equal(t, [2]any{float64(3), float64(0)}, [2]any{params["deliveries_expired"], params["processed"]})
	_, ok = stats.drain()
	assert.False(t, ok)
}

func assertNoResolution(t *testing.T, trace *nats.Subscription, reply string) {
	t.Helper()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		msg, err := trace.NextMsg(time.Until(deadline))
		if err == nats.ErrTimeout {
			return
		}
		require.NoError(t, err)
		if msg.Subject == reply {
			assert.Equal(t, "+WPI", string(msg.Data))
		}
	}
}

func TestPullPendingDataDoesNotPaceChatAtAckWait(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_FETCH_BATCH", "1")
	t.Setenv("NATS_PULL_ACK_WAIT", "2s")
	sub := openSubscriber(t, f, "worker")
	finished := make(chan *Completion, 12)
	require.NoError(t, Consume(t.Context(), nil, sub, hotLane, func(msg *Message) error {
		finished <- msg.DeferCompletion()
		return nil
	}, zap.NewNop()))
	started := time.Now()
	for range 12 {
		f.appendMessage(t, hotLane, "reply while DATA is down")
	}
	for range 12 {
		select {
		case completion := <-finished:
			defer completion.Complete(nil)
		case <-time.After(time.Second):
			t.Fatal("DATA completion paced chat processing")
		}
	}
	assert.Less(t, time.Since(started), time.Second)
}

func TestWaitForStreamSurvivesColdStartupAndCancellation(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(map[bool]string{true: "created later", false: "shutdown"}[create], func(t *testing.T) {
			f := completionFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- WaitForStream(ctx, f.url, DataStream) }()
			select {
			case err := <-done:
				t.Fatalf("wait ended before stream exists: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if create {
				spec := DataStream
				spec.Replicas = 1
				require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
			} else {
				cancel()
			}
			select {
			case err := <-done:
				assert.Equal(t, ctx.Err(), err)
			case <-time.After(time.Second):
				t.Fatal("stream wait did not finish")
			}
		})
	}
}
