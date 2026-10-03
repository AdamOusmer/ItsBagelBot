// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	t.Cleanup(h.unblockAndClose)
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

func (h *concurrentTestHarness) unblockAndClose() {
	h.stop.Do(func() { close(h.release); _ = h.subscriber.Close() })
}

func (h *concurrentTestHarness) assertBoundedHandlers(t *testing.T) {
	t.Helper()
	for range 3 {
		select {
		case msg := <-h.started:
			assert.False(t, signalClosed(msg.Acked()), "ACK before handler completion")
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
		failed := msg.UUID == "failed"
		assert.Equal(t, failed, signalClosed(msg.Nacked()), "%s NACK", msg.UUID)
		assert.Equal(t, !failed, signalClosed(msg.Acked()), "%s ACK", msg.UUID)
	}
}

func TestConsumeConcurrentBoundsHandlersAndAcknowledgesAfterResult(t *testing.T) {
	h := newConcurrentTestHarness(t)
	h.assertBoundedHandlers(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, h.consumer.Drain(ctx), context.Canceled)
	h.unblockAndClose()
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, h.consumer.Drain(ctx))

	h.assertResults(t)
}

func TestConsumeConcurrentRejectsInvalidWorkersAndSubscriptionErrors(t *testing.T) {
	s := &concurrentTestSubscriber{err: errors.New("subscribe failed")}

	_, invalidWorkers := ConsumeConcurrent(t.Context(), nil, s, "test", 0, nil, zap.NewNop())
	subscribedAfterInvalid := s.subscribed
	_, subscriptionErr := ConsumeConcurrent(t.Context(), nil, s, "test", 1, nil, zap.NewNop())

	require.Error(t, invalidWorkers)
	assert.False(t, subscribedAfterInvalid, "an invalid worker count must be rejected before subscribing")
	assert.ErrorIs(t, subscriptionErr, s.err)
}
