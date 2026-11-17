// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type interruptedDeadLetterPublisher struct {
	nats.JetStreamContext
	storeBeforeError bool
	calls            atomic.Int32
}

func (p *interruptedDeadLetterPublisher) PublishMsg(msg *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
	call := p.calls.Add(1)
	if call <= 2 && !p.storeBeforeError {
		return nil, nats.ErrNoResponders
	}
	ack, err := p.JetStreamContext.PublishMsg(msg, opts...)
	if call <= 2 {
		return nil, nats.ErrTimeout
	}
	return ack, err
}

func TestDeadLetterFailureRetainsSourceBeyondHandlerBudget(t *testing.T) {
	for _, tc := range []struct {
		name             string
		storeBeforeError bool
		malformed        bool
		deferral         bool
	}{
		{name: "publication unavailable"},
		{name: "publish acknowledgement lost", storeBeforeError: true},
		{name: "malformed source", malformed: true},
		{name: "terminal decision survives deferral slot", deferral: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBusFixture(t)
			cfg := LaneConfig{URL: f.url, Stream: DataStream.Name, Subject: "data.test.dlqretry", Group: uniqueGroup("dlqretry")}
			consumer := durableName(cfg.Group, cfg.Subject)
			_, err := f.js.AddConsumer(cfg.Stream, laneConsumerConfig(cfg.Subject, cfg.Group, consumer, 1))
			require.NoError(t, err)
			subscriber, err := NewLaneSubscriber(cfg, zap.NewNop())
			require.NoError(t, err)
			t.Cleanup(func() { _ = subscriber.Close() })
			s := subscriber.(*concurrentDurableSubscriber)
			if tc.deferral {
				s.delay = s.delay.withDeferral(time.Minute)
			}
			publisher := &interruptedDeadLetterPublisher{JetStreamContext: s.js, storeBeforeError: tc.storeBeforeError}
			s.js = publisher
			messages := subscribe(t, s, cfg.Subject)
			info, err := f.js.ConsumerInfo(cfg.Stream, s.consumer)
			require.NoError(t, err)
			require.Equal(t, -1, info.Config.MaxDeliver)
			reason := deadLetterMaxDeliveries
			if tc.malformed {
				reason = deadLetterMalformed
				f.appendMessage(t, cfg.Subject, "payload", "Traceparent", "one", "Traceparent", "two")
			} else {
				f.appendMessage(t, cfg.Subject, "payload", MessageIDHeader, "stable-event")
				require.True(t, receive(t, messages).Nack())
			}
			require.Eventually(t, func() bool { return publisher.calls.Load() >= 3 }, 15*time.Second, 10*time.Millisecond)
			require.Eventually(t, func() bool {
				info, err := f.js.ConsumerInfo(cfg.Stream, s.consumer)
				return err == nil && info.NumAckPending == 0
			}, time.Second, 10*time.Millisecond)
			letter, err := f.js.GetLastMsg(DataDeadLetterStream.Name, deadLetterSubject(cfg.Subject))
			require.NoError(t, err)
			assert.Equal(t, [2]string{"payload", string(reason)}, [2]string{string(letter.Data), letter.Header.Get(DeadLetterReasonHeader)})
			assert.Equal(t, uint64(1), f.storedMessages(t, DataDeadLetterStream.Name))
			requireSilence(t, messages, 100*time.Millisecond)
		})
	}
}

func TestTerminalDecisionSurvivesSubscriberReplacement(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal transfer", true: "requested deferral"}[deferred], func(t *testing.T) {
			f := newBusFixture(t)
			cfg := LaneConfig{URL: f.url, Stream: DataStream.Name, Subject: "data.test.replacement", Group: uniqueGroup("replacement")}
			first := replacementSubscriber(t, cfg)
			publisher := &interruptedDeadLetterPublisher{JetStreamContext: first.js}
			if !deferred {
				first.js = publisher
			}
			messages := subscribe(t, first, cfg.Subject)
			f.appendMessage(t, cfg.Subject, "payload")
			msg := receive(t, messages)
			if deferred {
				require.True(t, msg.nackAfter(500*time.Millisecond))
				require.Eventually(t, func() bool {
					return f.messageCount(dataDeferralStream) == 1
				}, time.Second, time.Millisecond)
			} else {
				require.True(t, msg.Nack())
				require.Eventually(t, func() bool { return publisher.calls.Load() == 1 }, time.Second, time.Millisecond)
			}
			require.NoError(t, first.Close())

			second := replacementSubscriber(t, cfg)
			replacement := subscribe(t, second, cfg.Subject)
			if deferred {
				require.True(t, receive(t, replacement).Nack())
			}
			require.Eventually(t, func() bool {
				return f.messageCount(DataDeadLetterStream) == 1
			}, 7*time.Second, time.Millisecond)
			info, err := f.js.StreamInfo(DataDeadLetterStream.Name)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, info.Config.Duplicates, DataStream.MaxAge)
			requireSilence(t, replacement, 100*time.Millisecond)
		})
	}
}

func replacementSubscriber(t *testing.T, cfg LaneConfig) *concurrentDurableSubscriber {
	t.Helper()
	subscriber, err := NewLaneSubscriber(cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = subscriber.Close() })
	s := subscriber.(*concurrentDurableSubscriber)
	s.delay = s.delay.withDeferral(time.Second)
	return s
}
