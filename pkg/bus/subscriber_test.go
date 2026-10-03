// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const (
	hotLane   = "twitch.ingress.event.premium"
	retryLane = "twitch.ingress.retry.premium"
)

type laneEnv struct {
	flow string
	mode string
}

var (
	explicitLanes = laneEnv{flow: "off"}
	pullLanes     = laneEnv{flow: "on", mode: "pull"}
	flowLanes     = laneEnv{flow: "on", mode: "flow"}
)

func (e laneEnv) apply(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_CONSUME_FLOW", e.flow)
	t.Setenv("NATS_CONSUME_MODE", e.mode)
	t.Setenv("NATS_PULL_CREATE_STAGGER", "0")
	t.Setenv("NATS_FLOW_RETRY_DELAY", "200ms")
}

func openSubscriber(t *testing.T, f busFixture, group string) Subscriber {
	t.Helper()
	sub, err := NewSubscriber(f.url, group, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func subscribe(t *testing.T, sub Subscriber, subject string) <-chan *Message {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	messages, err := sub.Subscribe(ctx, subject)
	require.NoError(t, err)
	return messages
}

func receive(t *testing.T, messages <-chan *Message) *Message {
	t.Helper()
	select {
	case msg, ok := <-messages:
		require.True(t, ok, "subscriber channel closed")
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a delivery")
		return nil
	}
}

func requireSilence(t *testing.T, messages <-chan *Message, wait time.Duration) {
	t.Helper()
	select {
	case msg := <-messages:
		t.Fatalf("unexpected delivery %q", msg.UUID)
	case <-time.After(wait):
	}
}

func uniqueGroup(prefix string) string { return prefix + "-" + nuid.Next() }

func TestSubscriberDeliversStoredMessagesInEveryLaneMode(t *testing.T) {
	f := newBusFixture(t)

	for _, tc := range []struct {
		name string
		mode laneEnv
	}{
		{"explicit acknowledgements", explicitLanes},
		{"pull lane", pullLanes},
		{"flow-control lane", flowLanes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mode.apply(t)
			messages := subscribe(t, openSubscriber(t, f, uniqueGroup("deliver")), hotLane)
			f.appendMessage(t, hotLane, `{"text":"hello"}`, MessageIDHeader, "fleet-id", "Traceparent", "00-trace-span-01")

			msg := receive(t, messages)

			assert.Equal(t, "fleet-id", msg.UUID)
			assert.Equal(t, `{"text":"hello"}`, string(msg.Payload))
			assert.Equal(t, Metadata{"Traceparent": "00-trace-span-01"}, msg.Metadata, "only application headers are metadata")
			assert.WithinDuration(t, time.Now(), msg.StoredAt(), time.Minute, "the stored-at time comes from the ack reply")
			assert.True(t, msg.Ack())
			requireSilence(t, messages, 300*time.Millisecond)
		})
	}
}

func TestNackedHotLaneMessageIsScheduledForExactlyOneRetryHop(t *testing.T) {
	f := newBusFixture(t)

	for _, tc := range []struct {
		name string
		mode laneEnv
	}{
		{"pull lane", pullLanes},
		{"flow-control lane", flowLanes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mode.apply(t)
			messages := subscribe(t, openSubscriber(t, f, uniqueGroup("retry")), hotLane)
			retries := subscribe(t, openSubscriber(t, f, uniqueGroup("retry-lane")), retryLane)
			f.appendMessage(t, hotLane, `{}`, MessageIDHeader, "first-hop")
			require.True(t, receive(t, messages).Nack(), "the first Nack must win")

			retried := receive(t, retries)
			require.True(t, retried.Ack())
			require.NoError(t, f.js.PurgeStream(TwitchIngressRetryStream.Name))
			f.appendMessage(t, hotLane, `{}`, MessageIDHeader, "second-hop", RetryCountHeader, "1")
			require.True(t, receive(t, messages).Nack())

			assert.Equal(t, "first-hop", retried.UUID)
			assert.Equal(t, "1", retried.Metadata.Get(RetryCountHeader), "the retry carries its hop count")
			require.Never(t, func() bool { return f.messageCount(TwitchIngressRetryStream) > 0 },
				time.Second, 50*time.Millisecond, "a message that already used its retry hop must be dropped, not rescheduled")
		})
	}
}

func TestPullLaneResumesAfterAnExplicitDurableWithoutReplayingAckedWork(t *testing.T) {
	f := newBusFixture(t)
	group := uniqueGroup("flip")
	explicitLanes.apply(t)
	explicit, err := NewSubscriber(f.url, group, zap.NewNop())
	require.NoError(t, err)
	messages := subscribe(t, explicit, hotLane)
	for i := range 3 {
		f.appendMessage(t, hotLane, `{}`, MessageIDHeader, "before-"+string(rune('a'+i)))
		require.True(t, receive(t, messages).Ack())
	}
	_ = explicit.Close()
	f.appendMessage(t, hotLane, `{}`, MessageIDHeader, "after-a")
	f.appendMessage(t, hotLane, `{}`, MessageIDHeader, "after-b")

	pullLanes.apply(t)
	resumed := subscribe(t, openSubscriber(t, f, group), hotLane)

	first, second := receive(t, resumed), receive(t, resumed)
	assert.Equal(t, []string{"after-a", "after-b"}, []string{first.UUID, second.UUID})
	assert.True(t, first.Ack() && second.Ack())
	requireSilence(t, resumed, 500*time.Millisecond)
}

func TestMalformedDataMessageIsDeadLetteredInsteadOfDelivered(t *testing.T) {
	f := newBusFixture(t)
	explicitLanes.apply(t)
	group := uniqueGroup("dlq")
	messages := subscribe(t, openSubscriber(t, f, group), "data.test.malformed")

	f.appendMessage(t, "data.test.malformed", `{}`, "Traceparent", "one", "Traceparent", "two")

	requireSilence(t, messages, time.Second)
	letter, err := f.js.GetLastMsg(BagelDeadLetterStream.Name, "dlq.data.test.malformed")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		DeadLetterSubjectHeader: "data.test.malformed",
		DeadLetterStreamHeader:  BagelDataStream.Name,
		DeadLetterReasonHeader:  "malformed",
	}, map[string]string{
		DeadLetterSubjectHeader: letter.Header.Get(DeadLetterSubjectHeader),
		DeadLetterStreamHeader:  letter.Header.Get(DeadLetterStreamHeader),
		DeadLetterReasonHeader:  letter.Header.Get(DeadLetterReasonHeader),
	})
	assert.Equal(t, uint64(1), f.storedMessages(t, BagelDeadLetterStream.Name))
}

func TestAcknowledgedWorkQueueMessageLeavesTheStream(t *testing.T) {
	f := newBusFixture(t)
	explicitLanes.apply(t)
	messages := subscribe(t, openSubscriber(t, f, "work-queue-lane"), "twitch.outgress.premium")
	f.appendMessage(t, "twitch.outgress.premium", `{}`, MessageIDHeader, "outbound")

	msg := receive(t, messages)
	require.Equal(t, "outbound", msg.UUID)
	require.Equal(t, uint64(1), f.storedMessages(t, OutgressStream.Name))
	require.True(t, msg.Ack())

	require.Eventually(t, func() bool { return f.messageCount(OutgressStream) == 0 },
		5*time.Second, 25*time.Millisecond, "an acknowledged work-queue message must be removed")
}

func TestBroadcastSubscribersEachReceiveNewMessages(t *testing.T) {
	f := newBusFixture(t)
	first := subscribe(t, openSubscriber(t, f, ""), "data.test.broadcast")
	second := subscribe(t, openSubscriber(t, f, ""), "data.test.broadcast")

	f.appendMessage(t, "data.test.broadcast", "fanout", MessageIDHeader, "broadcast-id")

	for _, messages := range []<-chan *Message{first, second} {
		msg := receive(t, messages)
		assert.Equal(t, [2]string{"broadcast-id", "fanout"}, [2]string{msg.UUID, string(msg.Payload)})
		assert.True(t, msg.Ack())
	}
}

func TestSubscriberRejectsUnclaimedSubjectsAndClosedUse(t *testing.T) {
	f := newBusFixture(t)
	sub, err := NewSubscriber(f.url, uniqueGroup("closed"), zap.NewNop())
	require.NoError(t, err)

	_, unclaimed := sub.Subscribe(context.Background(), "nobody.claims.this")
	require.NoError(t, sub.Close())
	_, closed := sub.Subscribe(context.Background(), "twitch.outgress.standard")

	assert.Error(t, unclaimed)
	assert.ErrorContains(t, closed, "closed")
	assert.NoError(t, sub.Close(), "a second Close is a no-op")
}

func TestLaneHealthReportsReadyWhileTheLaneFetches(t *testing.T) {
	f := newBusFixture(t)
	pullLanes.apply(t)
	sub := openSubscriber(t, f, uniqueGroup("health"))
	subscribe(t, sub, hotLane)

	assert.True(t, SubscriberHealthy(sub))
	assert.NoError(t, LaneCheck("premium", sub).Probe(context.Background()))
}
