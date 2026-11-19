// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func progressTrace(t *testing.T, f busFixture) *nats.Subscription {
	t.Helper()
	nc, err := nats.Connect(f.url)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	trace, err := nc.SubscribeSync("$JS.ACK.>")
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	return trace
}

type ackFilter struct {
	reply    string
	progress bool
}

func (f ackFilter) matches(msg *nats.Msg) bool {
	if f.reply != "" && msg.Subject != f.reply {
		return false
	}
	return (string(msg.Data) == "+WPI") == f.progress
}

func awaitPullAck(t *testing.T, trace *nats.Subscription, filter ackFilter) *nats.Msg {
	t.Helper()
	for {
		msg, err := trace.NextMsg(2 * time.Second)
		require.NoError(t, err)
		if filter.matches(msg) {
			return msg
		}
	}
}

func TestPullProgressHonorsEachDeliveryTouchTime(t *testing.T) {
	f := completionFixture(t)
	trace := progressTrace(t, f)
	messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	f.appendMessage(t, hotLane, "first")
	first := receive(t, messages)
	firstReply := awaitPullAck(t, trace, ackFilter{progress: true}).Subject
	f.appendMessage(t, hotLane, "young")
	young := receive(t, messages)
	first.Ack()
	awaitPullAck(t, trace, ackFilter{reply: firstReply})
	awaitPullAck(t, trace, ackFilter{progress: true})
	assert.GreaterOrEqual(t, time.Since(young.DeliveredAt()), time.Second/3-10*time.Millisecond)
	young.Ack()
}

func TestPullProgressEndsBeforeRetentionAndKeepsExpiryUnhealthy(t *testing.T) {
	for _, age := range []time.Duration{1400 * time.Millisecond, 2 * time.Second} {
		t.Run(age.String(), func(t *testing.T) { testProgressExpiry(t, age) })
	}
}

func testProgressExpiry(t *testing.T, age time.Duration) {
	t.Helper()
	f := completionFixture(t)
	t.Setenv("NATS_PULL_ACK_WAIT", "600ms")
	spec := TwitchIngressPremiumStream
	spec.MaxAge, spec.Replicas = 3*time.Second, 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	f.appendMessage(t, hotLane, "stalled completion")
	time.Sleep(age)
	first := openSubscriber(t, f, "worker")
	messages := subscribe(t, first, hotLane)
	msg := receive(t, messages)
	defer msg.Ack()
	require.Eventually(t, func() bool { return !SubscriberHealthy(first) }, 1300*time.Millisecond, time.Millisecond)
	second := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	redelivered := receiveEither(t, messages, second)
	assert.Equal(t, msg.UUID, redelivered.UUID)
	require.True(t, DeliveryAlive(redelivered.WithDeliveryContext(t.Context())))
	redelivered.Ack()
	require.Eventually(t, func() bool { return f.messageCount(spec) == 0 }, 100*time.Millisecond, time.Millisecond)
	require.Never(t, func() bool { return SubscriberHealthy(first) }, 300*time.Millisecond, time.Millisecond)
	msg.Ack()
}

func receiveEither(t *testing.T, first, second <-chan *Message) *Message {
	t.Helper()
	select {
	case msg := <-first:
		return msg
	case msg := <-second:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("pending work was not redelivered within retention")
		return nil
	}
}

func TestPullCompletionForgetsBeforeAcknowledgement(t *testing.T) {
	for _, ack := range []bool{true, false} {
		t.Run(map[bool]string{true: "ACK", false: "NAK"}[ack], func(t *testing.T) {
			f := completionFixture(t)
			trace := progressTrace(t, f)
			messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
			f.appendMessage(t, hotLane, "completion")
			msg := receive(t, messages)
			reply := awaitPullAck(t, trace, ackFilter{progress: true}).Subject
			started := time.Now()
			resolveTestMessage(msg, ack)
			awaitPullAck(t, trace, ackFilter{reply: reply})
			if !ack {
				receive(t, messages).Ack()
				assert.GreaterOrEqual(t, time.Since(started), 180*time.Millisecond)
			}
			assertNoProgress(t, trace, reply)
		})
	}
}

func resolveTestMessage(msg *Message, ack bool) {
	if ack {
		msg.Ack()
		return
	}
	msg.Nack()
}

func assertNoProgress(t *testing.T, trace *nats.Subscription, reply string) {
	t.Helper()
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		msg, err := trace.NextMsg(time.Until(deadline))
		if err == nats.ErrTimeout {
			return
		}
		require.NoError(t, err)
		assert.False(t, msg.Subject == reply && strings.HasPrefix(string(msg.Data), "+WPI"))
	}
}

func TestPullCloseReturnsFetchedButUndeliveredBatch(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_FETCH_BATCH", "3")
	first := openSubscriber(t, f, "worker")
	_ = subscribe(t, first, hotLane)
	for _, body := range []string{"first", "second", "third"} {
		f.appendMessage(t, hotLane, body)
	}
	name := PullLaneConsumerConfig("worker", hotLane).Name
	require.Eventually(t, func() bool {
		info, err := f.js.ConsumerInfo(TwitchIngressPremiumStream.Name, name)
		return err == nil && info.NumAckPending == 3
	}, time.Second, time.Millisecond)
	require.NoError(t, first.Close())
	started := time.Now()
	second := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	for range 3 {
		receive(t, second).Ack()
	}
	assert.Less(t, time.Since(started), 800*time.Millisecond)
}
