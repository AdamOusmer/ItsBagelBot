// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type deadlineTiming struct {
	ackWait, watchTimeout, redeliveryTimeout time.Duration
	real                                     bool
}

func deadlineTestTiming() deadlineTiming {
	if os.Getenv("BUS_REAL_DEADLINE") == "1" {
		return deadlineTiming{laneAckWait, 35 * time.Second, 8 * time.Second, true}
	}
	return deadlineTiming{300 * time.Millisecond, 2 * time.Second, 3 * time.Second, false}
}

func deadlineTestJetStream(t *testing.T, ackWait time.Duration) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	require.NoError(t, err)
	srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second))
	t.Cleanup(srv.Shutdown)
	nc, err := nats.Connect(srv.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	require.NoError(t, err)
	_, err = js.AddStream(&nats.StreamConfig{Name: "TEST", Subjects: []string{"test.deadline"}, Storage: nats.MemoryStorage})
	require.NoError(t, err)
	_, err = js.AddConsumer("TEST", &nats.ConsumerConfig{Durable: "worker", DeliverGroup: "worker", DeliverSubject: nats.NewInbox(), AckPolicy: nats.AckExplicitPolicy, AckWait: ackWait})
	require.NoError(t, err)
	return nc, js
}

func deadlineTestSubscriber(t *testing.T, nc *nats.Conn, js nats.JetStreamContext, timing deadlineTiming) (*concurrentDurableSubscriber, <-chan *Message) {
	t.Helper()
	sub := newConcurrentDurableSubscriber(concurrentSubscriberConfig{nc: nc, js: js, stream: "TEST", consumer: "worker", group: "worker", log: zap.NewNop()})
	if !timing.real {
		sub.handlerDeadline = 400 * time.Millisecond
		sub.wheel = newKeepAliveWheel(20, 20*time.Millisecond, sub.closeCh)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = sub.Close() })
	deliveries, err := sub.Subscribe(ctx, "test.deadline")
	require.NoError(t, err)
	return sub, deliveries
}

func receiveDeadlineMessage(t *testing.T, deliveries <-chan *Message, timeout time.Duration) *Message {
	t.Helper()
	select {
	case msg := <-deliveries:
		require.NotNil(t, msg)
		return msg
	case <-time.After(timeout):
		t.Fatal("delivery timed out")
		return nil
	}
}

// Test the production subscriber and resultWatch against JetStream, shortening
// only the test's wheel and consumer AckWait unless BUS_REAL_DEADLINE is set.
func TestHandlerDeadlineNacksBeforeWatchExpiry(t *testing.T) {
	timing := deadlineTestTiming()
	nc, js := deadlineTestJetStream(t, timing.ackWait)
	_, deliveries := deadlineTestSubscriber(t, nc, js, timing)
	_, err := js.Publish("test.deadline", []byte("first"))
	require.NoError(t, err)
	first := receiveDeadlineMessage(t, deliveries, 2*time.Second)
	_, hasDeadline := first.Context().Deadline()
	require.True(t, hasDeadline)
	select {
	case <-first.Context().Done():
	case <-time.After(timing.watchTimeout):
		t.Fatal("handler deadline did not expire")
	}
	require.ErrorIs(t, first.Context().Err(), context.DeadlineExceeded)
	require.True(t, first.Nack())
	second := receiveDeadlineMessage(t, deliveries, timing.redeliveryTimeout)
	require.Equal(t, first.UUID, second.UUID)
	require.True(t, second.Ack())
	require.Eventually(t, func() bool {
		info, e := js.ConsumerInfo("TEST", "worker")
		return e == nil && info.NumAckPending == 0 && info.AckFloor.Stream == 1
	}, 2*time.Second, 20*time.Millisecond)
	require.ErrorIs(t, second.Context().Err(), context.Canceled)
}
