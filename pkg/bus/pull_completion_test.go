// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func completionFixture(t *testing.T) busFixture {
	t.Helper()
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	t.Setenv("NATS_PULL_REPLICAS", "1")
	t.Setenv("NATS_PULL_CREATE_STAGGER", "0")
	t.Setenv("NATS_PULL_ACK_WAIT", "1s")
	t.Setenv("NATS_PULL_NAK_DELAY", "200ms")
	url := privateBroker(t)
	js := jetStreamOf(t, url)
	spec := TwitchIngressPremiumStream
	spec.Replicas = 1
	require.NoError(t, EnsureStreams(t.Context(), url, []StreamSpec{spec}, zap.NewNop()))
	return busFixture{url: url, js: js}
}

func TestPullDrainHeartbeatsUntilUncompletedWorkIsReleased(t *testing.T) {
	f := completionFixture(t)
	first := openSubscriber(t, f, "worker")
	messages := subscribe(t, first, hotLane)
	f.appendMessage(t, hotLane, "payload", MessageIDHeader, "unfinished")
	msg := receive(t, messages)
	closed := make(chan error, 1)
	go func() { closed <- first.Close() }()
	<-msg.Context().Done()
	second := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
	requireSilence(t, second, 1200*time.Millisecond)
	started := time.Now()
	msg.Nack()
	require.NoError(t, <-closed)
	redelivered := receive(t, second)
	assert.Equal(t, msg.UUID, redelivered.UUID)
	assert.GreaterOrEqual(t, time.Since(started), 180*time.Millisecond)
	require.True(t, redelivered.Ack())
}

func TestPullCompletionControlsAckAndDelayedNak(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "failure"}[fail], func(t *testing.T) {
			f := completionFixture(t)
			sub := openSubscriber(t, f, "worker")
			tokens := make(chan *Completion, 2)
			_, err := ConsumeDemand(context.Background(), DemandConsumerConfig{
				Subscriber: sub, Subject: hotLane, Log: zap.NewNop(),
				Handle: func(msg *Message) error { tokens <- msg.DeferCompletion(); return nil },
			})
			require.NoError(t, err)
			f.appendMessage(t, hotLane, "payload")
			completion := <-tokens
			time.Sleep(80 * time.Millisecond)
			assert.Equal(t, uint64(1), f.messageCount(TwitchIngressPremiumStream), "handler return cannot acknowledge deferred work")
			started := time.Now()
			var outcome error
			if fail {
				outcome = errors.New("transaction failed")
			}
			require.True(t, completion.Complete(outcome))
			if fail {
				retry := <-tokens
				assert.GreaterOrEqual(t, time.Since(started), 180*time.Millisecond)
				retry.Complete(nil)
			}
			require.Eventually(t, func() bool { return f.messageCount(TwitchIngressPremiumStream) == 0 }, time.Second, 10*time.Millisecond)
		})
	}
}

func TestHotPoisonNeedsDeadLetterPubAck(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler poison", true: "malformed headers"}[malformed], func(t *testing.T) {
			f := completionFixture(t)
			logs, observed := observer.New(zap.WarnLevel)
			sub, err := NewSubscriber(f.url, "worker", zap.New(logs))
			require.NoError(t, err)
			t.Cleanup(func() { _ = sub.Close() })
			_, err = ConsumeDemand(context.Background(), DemandConsumerConfig{
				Subscriber: sub, Subject: hotLane, Log: zap.NewNop(),
				Handle: func(*Message) error { return Poison(errors.New("invalid envelope")) },
			})
			require.NoError(t, err)
			headers := []string{MessageIDHeader, "poison"}
			if malformed {
				headers = append(headers, "Traceparent", "one", "Traceparent", "two")
			}
			f.appendMessage(t, hotLane, "invalid", headers...)
			require.Eventually(t, func() bool {
				return observed.FilterMessage("dead letter publish failed; retaining source delivery").Len() > 0
			}, 3*time.Second, 10*time.Millisecond)
			require.Equal(t, uint64(1), f.messageCount(TwitchIngressPremiumStream), "failed DLQ transfer must retain source")
			spec := IngressDeadLetterStream
			spec.Replicas = 1
			require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
			require.Eventually(t, func() bool { return f.messageCount(TwitchIngressPremiumStream) == 0 }, 8*time.Second, 20*time.Millisecond)
			letter, err := f.js.GetLastMsg(IngressDeadLetterStream.Name, IngressDeadLetterPrefix+hotLane)
			require.NoError(t, err)
			assert.Equal(t, []byte("invalid"), letter.Data)
			assert.Equal(t, TwitchIngressPremiumStream.Name, letter.Header.Get(DeadLetterStreamHeader))
		})
	}
}

func TestStreamReconcileAppliesInterestAndDiscardOld(t *testing.T) {
	f := completionFixture(t)
	info, err := f.js.StreamInfo(TwitchIngressPremiumStream.Name)
	require.NoError(t, err)
	info.Config.Retention, info.Config.Discard = nats.LimitsPolicy, nats.DiscardNew
	_, err = f.js.UpdateStream(&info.Config)
	require.NoError(t, err)
	spec := TwitchIngressPremiumStream
	spec.Replicas = 1
	require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
	info, err = f.js.StreamInfo(spec.Name)
	require.NoError(t, err)
	assert.Equal(t, [2]any{nats.InterestPolicy, nats.DiscardOld}, [2]any{info.Config.Retention, info.Config.Discard})
}
