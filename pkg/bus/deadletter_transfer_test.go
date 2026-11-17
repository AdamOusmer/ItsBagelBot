// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type slowDeadLetterPublisher struct {
	nats.JetStreamContext
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (p *slowDeadLetterPublisher) PublishMsg(msg *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error) {
	switch p.calls.Add(1) {
	case 1:
		return nil, nats.ErrNoResponders
	case 2:
		close(p.started)
		<-p.release
	}
	return p.JetStreamContext.PublishMsg(msg, opts...)
}

func TestSlowTerminalTransferDoesNotBlockLaneCallback(t *testing.T) {
	f := newBusFixture(t)
	cfg := LaneConfig{URL: f.url, Stream: DataStream.Name, Subject: "data.test.slowdlq", Group: uniqueGroup("slowdlq")}
	subscriber, err := NewLaneSubscriber(cfg, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = subscriber.Close() })
	s := subscriber.(*concurrentDurableSubscriber)
	publisher := &slowDeadLetterPublisher{JetStreamContext: s.js, started: make(chan struct{}), release: make(chan struct{})}
	s.js = publisher
	s.delay = s.delay.withDeferral(time.Minute)
	defer close(publisher.release)
	messages := subscribe(t, s, cfg.Subject)
	f.appendMessage(t, cfg.Subject, "poison")
	require.True(t, receive(t, messages).Nack())
	select {
	case <-publisher.started:
	case <-time.After(7 * time.Second):
		t.Fatal("terminal transfer was not retried without invoking the handler")
	}
	f.appendMessage(t, cfg.Subject, "healthy")
	select {
	case msg := <-messages:
		assert.Equal(t, "healthy", string(msg.Payload))
		require.True(t, msg.Ack())
	case <-time.After(time.Second):
		t.Fatal("slow dead letter publication blocked the lane")
	}
}

func TestDeadLetterDeduplicationIncludesConsumer(t *testing.T) {
	f := newBusFixture(t)
	const subject = "data.test.dlqgroups"
	var sources []<-chan *Message
	var consumers []string
	for range 2 {
		cfg := LaneConfig{URL: f.url, Stream: DataStream.Name, Subject: subject, Group: uniqueGroup("dlqgroup")}
		subscriber, err := NewLaneSubscriber(cfg, zap.NewNop())
		require.NoError(t, err)
		t.Cleanup(func() { _ = subscriber.Close() })
		sources = append(sources, subscribe(t, subscriber, subject))
		consumers = append(consumers, durableName(cfg.Group, cfg.Subject))
	}
	f.appendMessage(t, subject, "payload")
	for _, source := range sources {
		require.True(t, receive(t, source).Nack())
	}
	require.Equal(t, uint64(2), f.storedMessages(t, DataDeadLetterStream.Name))
	info, err := f.js.StreamInfo(DataDeadLetterStream.Name)
	require.NoError(t, err)
	var actual []string
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		letter, err := f.js.GetMsg(DataDeadLetterStream.Name, seq)
		require.NoError(t, err)
		actual = append(actual, letter.Header.Get(DeadLetterConsumerHeader))
	}
	assert.ElementsMatch(t, consumers, actual)
}

func TestUndeliverableDeadLetterLogsSourceExpiry(t *testing.T) {
	for _, tc := range []struct {
		name string
		age  time.Duration
		logs int
	}{
		{"fresh", time.Second, 0},
		{"expiring before the next transfer", DataStream.MaxAge - deadLetterRetryDelay, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.ErrorLevel)
			s := &concurrentDurableSubscriber{stream: DataStream.Name, consumer: "consumer", log: zap.New(core)}
			msg := &nats.Msg{Subject: "data.test.expiry", Reply: fmt.Sprintf("$JS.ACK.DATA.consumer.1.1.1.%d.0", time.Now().Add(-tc.age).UnixNano())}
			s.retryDeadLetter(msg, nats.ErrNoResponders)
			assert.Equal(t, tc.logs, logs.FilterMessage("source message approaching DATA MaxAge without a dead letter").Len())
		})
	}
}
