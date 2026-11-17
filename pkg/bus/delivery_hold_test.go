// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDeliveryHoldStartsAfterHandlerExecution(t *testing.T) {
	for _, returned := range []bool{false, true} {
		t.Run(map[bool]string{false: "executing", true: "reporter"}[returned], func(t *testing.T) {
			f := completionFixture(t)
			t.Setenv("NATS_PULL_ACK_WAIT", "100ms")
			messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
			f.appendMessage(t, hotLane, "held")
			msg := receive(t, messages)
			HoldDelivery(msg.Context())
			if returned {
				EndDeliveryExecution(msg.Context())
				requireSilence(t, messages, 500*time.Millisecond)
				require.True(t, DeliveryAlive(msg.Context()))
				msg.Ack()
				return
			}
			retry := receive(t, messages)
			require.Equal(t, msg.UUID, retry.UUID)
			require.False(t, DeliveryAlive(msg.Context()))
			retry.Ack()
			msg.Nack()
		})
	}
}

func TestHeldDeliveryStopsSourceProgressWithoutCompleting(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_ACK_WAIT", "100ms")
	spec := TwitchIngressPremiumStream
	spec.MaxAge, spec.Replicas = 600*time.Millisecond, 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	trace := progressTrace(t, f)
	sub := openSubscriber(t, f, "worker")
	messages := subscribe(t, sub, hotLane)
	f.appendMessage(t, hotLane, "held generation")
	msg := receive(t, messages)
	ctx := msg.WithDeliveryContext(t.Context())
	HoldDelivery(ctx)
	EndDeliveryExecution(ctx)
	reply := awaitPullAck(t, trace, ackFilter{progress: true}).Subject
	require.Eventually(t, func() bool { return DeliverySourceExpired(ctx) }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		c := msg.completionState()
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.reject == nil
	}, time.Second, time.Millisecond)
	_, err := trace.NextMsg(50 * time.Millisecond)
	for err == nil {
		_, err = trace.NextMsg(50 * time.Millisecond)
	}
	assertNoProgress(t, trace, reply)
	require.True(t, SubscriberHealthy(sub))
	require.False(t, DeliveryAlive(ctx))
	require.True(t, DeliveryTerminal(ctx))
	completion := msg.DeferCompletion()
	select {
	case <-msg.Acked():
		t.Fatal("expired source completed before its generation")
	default:
	}
	require.True(t, completion.Complete(nil))
}

func TestDisconnectedDeliveryCannotClaimLiveness(t *testing.T) {
	f := completionFixture(t)
	nc, err := nats.Connect(f.url)
	require.NoError(t, err)
	s := &pullSubscriber{nc: nc}
	s.pendingShard("reply").deliveries = map[string]*DeliveryTicket{"reply": {reply: "reply", stored: time.Now().UnixNano(), held: true, executed: true}}
	ctx := s.deliveryContext(s.deliveryTicket("reply"))
	require.True(t, DeliveryAlive(ctx))
	nc.Close()
	require.False(t, DeliveryAlive(ctx))
	require.False(t, DeliverySourceExpired(ctx))
}

func TestHeartbeatPublicationDoesNotWaitForCompletionLock(t *testing.T) {
	f := completionFixture(t)
	nc, err := nats.Connect(f.url)
	require.NoError(t, err)
	defer nc.Close()
	now := time.Now()
	s := &pullSubscriber{nc: nc, log: zap.NewNop()}
	s.pendingShard("reply").deliveries = map[string]*DeliveryTicket{
		"reply": {reply: "reply", stored: now.UnixNano(), touched: -1000, held: true, executed: true},
	}
	s.desired.AckWait = time.Second
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	done := make(chan struct{})
	go func() { s.refreshPending(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("progress publication waited for the completion lock")
	}
}

func TestHotDeliveryReceiptRetentionUsesItsSourceHorizon(t *testing.T) {
	f := completionFixture(t)
	t.Setenv("NATS_PULL_ACK_WAIT", "20s")
	messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	f.appendMessage(t, hotLane, "retention")
	msg := receive(t, messages)
	require.Equal(t, TwitchEffectRetention(), DeliveryEffectRetention(msg.Context()))
	msg.Ack()
}

func TestReporterProgressUsesTwoThirdsOfAckWait(t *testing.T) {
	for _, tc := range []struct {
		name  string
		held  bool
		async int32
	}{
		{"executing", false, 0}, {"reporter", true, 0}, {"async", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			s := &pullSubscriber{}
			s.desired.AckWait = 3 * time.Second
			shard := s.pendingShard("reply")
			shard.deliveries = map[string]*DeliveryTicket{"reply": {stored: now.UnixNano(), held: tc.held, executed: tc.held, async: tc.async}}
			var got []int
			for second := range 5 {
				progress := s.progressReplies(shard, now.Add(time.Duration(second+1)*time.Second))
				got = append(got, len(progress.replies))
			}
			want := []int{1, 1, 1, 1, 1}
			if tc.held && tc.async == 0 {
				want = []int{0, 1, 0, 1, 0}
			}
			require.Equal(t, want, got)
		})
	}
}
