// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type concurrentTestSubscriber struct {
	messages   chan *Message
	err        error
	subscribed bool
}

func (s *concurrentTestSubscriber) Subscribe(context.Context, string) (<-chan *Message, error) {
	s.subscribed = true
	return s.messages, s.err
}
func (s *concurrentTestSubscriber) Close() error { close(s.messages); return nil }

type concurrentTestHarness struct {
	subscriber *concurrentTestSubscriber
	started    chan *Message
	release    chan struct{}
	consumer   *ConcurrentConsumer
	messages   []*Message
	stop       sync.Once
}

func newConcurrentTestHarness(t *testing.T) *concurrentTestHarness {
	t.Helper()
	h := &concurrentTestHarness{
		subscriber: &concurrentTestSubscriber{messages: make(chan *Message, 5)},
		started:    make(chan *Message, 5),
		release:    make(chan struct{}),
		messages:   []*Message{NewMessage("one", nil), NewMessage("failed", nil), NewMessage("three", nil), NewMessage("four", nil), NewMessage("five", nil)},
	}
	consumer, err := ConsumeConcurrent(t.Context(), nil, h.subscriber, "test.concurrent.counter", 3, h.handle, zap.NewNop())
	require.NoError(t, err)
	h.consumer = consumer
	t.Cleanup(h.finish)
	for _, msg := range h.messages {
		h.subscriber.messages <- msg
	}
	return h
}

func (h *concurrentTestHarness) handle(msg *Message) error {
	h.started <- msg
	<-h.release
	if msg.UUID == "failed" {
		return errors.New("SQL rolled back")
	}
	return nil
}

func (h *concurrentTestHarness) finish() {
	h.stop.Do(func() { close(h.release); _ = h.subscriber.Close() })
}

func assertNoSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(message)
	default:
	}
}

func assertSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	default:
		t.Fatal(message)
	}
}

func (h *concurrentTestHarness) assertBoundedHandlers(t *testing.T) {
	t.Helper()
	for range 3 {
		select {
		case msg := <-h.started:
			assertNoSignal(t, msg.Acked(), "ACK before handler completion")
		case <-time.After(time.Second):
			t.Fatal("handlers did not run concurrently")
		}
	}
	select {
	case <-h.started:
		t.Fatal("exceeded fixed worker count")
	default:
	}
}

func (h *concurrentTestHarness) assertResults(t *testing.T) {
	t.Helper()
	for _, msg := range h.messages {
		if msg.UUID == "failed" {
			assertSignal(t, msg.Nacked(), "failed write was not NACKed")
			continue
		}
		assertSignal(t, msg.Acked(), "successful write was not ACKed")
	}
}

func TestConsumeConcurrentBoundsHandlersAndAcknowledgesAfterResult(t *testing.T) {
	h := newConcurrentTestHarness(t)
	h.assertBoundedHandlers(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, h.consumer.Drain(ctx), context.Canceled)
	h.finish()
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, h.consumer.Drain(ctx))
	h.assertResults(t)
}

func TestConsumeConcurrentRejectsInvalidWorkersAndSubscriptionErrors(t *testing.T) {
	s := &concurrentTestSubscriber{err: errors.New("subscribe failed")}
	_, err := ConsumeConcurrent(t.Context(), nil, s, "test", 0, nil, zap.NewNop())
	require.Error(t, err)
	require.False(t, s.subscribed)
	_, err = ConsumeConcurrent(t.Context(), nil, s, "test", 1, nil, zap.NewNop())
	require.ErrorIs(t, err, s.err)
}
