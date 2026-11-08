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

// Test the production subscriber and resultWatch against JetStream, shortening
// only the test's wheel and consumer AckWait to avoid a 34-second test.
func TestObservedExpiredWatchIgnoresLateAckAndRedelivers(t *testing.T) {
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
	ackWait := 300 * time.Millisecond
	if os.Getenv("BUS_REAL_DEADLINE") == "1" {
		ackWait = laneAckWait
	}
	_, err = js.AddConsumer("TEST", &nats.ConsumerConfig{Durable: "worker", DeliverGroup: "worker", DeliverSubject: nats.NewInbox(), AckPolicy: nats.AckExplicitPolicy, AckWait: ackWait})
	require.NoError(t, err)
	sub := newConcurrentDurableSubscriber(concurrentSubscriberConfig{nc: nc, js: js, stream: "TEST", consumer: "worker", group: "worker", log: zap.NewNop()})
	watchTimeout := 2 * time.Second
	redeliveryTimeout := 3 * time.Second
	if os.Getenv("BUS_REAL_DEADLINE") == "1" {
		watchTimeout = 35 * time.Second
		redeliveryTimeout = 8 * time.Second
	} else {
		sub.wheel = newKeepAliveWheel(20, 20*time.Millisecond, sub.closeCh)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = sub.Close() })
	deliveries, err := sub.Subscribe(ctx, "test.deadline")
	require.NoError(t, err)
	_, err = js.Publish("test.deadline", []byte("first"))
	require.NoError(t, err)
	var first *Message
	select {
	case first = <-deliveries:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery timed out")
	}
	_, hasDeadline := first.Context().Deadline()
	require.False(t, hasDeadline, "subscriber supplies an unbounded subscription context")
	// Wait for the actual production resultWatch to finish, without peeking
	// into or advancing its wheel. The consumer remains unacknowledged.
	var watch *resultWatch
	require.Eventually(t, func() bool {
		sub.wheel.mu.Lock()
		defer sub.wheel.mu.Unlock()
		for _, bucket := range sub.wheel.buckets {
			for _, candidate := range bucket {
				if candidate.msg == first {
					watch = candidate
					return true
				}
			}
		}
		return false
	}, 300*time.Millisecond, 5*time.Millisecond)
	require.Eventually(t, watch.finished.Load, watchTimeout, 10*time.Millisecond)
	require.NoError(t, first.Context().Err(), "handler remains live after keepalive expires")
	require.True(t, first.Ack(), "local acknowledgement is accepted")
	var second *Message
	select {
	case second = <-deliveries:
	case <-time.After(redeliveryTimeout):
		t.Fatal("late local ACK unexpectedly prevented redelivery")
	}
	require.Equal(t, first.UUID, second.UUID)
	require.NoError(t, first.Context().Err(), "the first handler can still run concurrently with redelivery")
	require.True(t, second.Ack())
	require.Eventually(t, func() bool {
		info, e := js.ConsumerInfo("TEST", "worker")
		return e == nil && info.NumAckPending == 0 && info.AckFloor.Stream == 1
	}, 2*time.Second, 20*time.Millisecond)
}
