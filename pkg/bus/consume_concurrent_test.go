// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
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

func TestConsumeConcurrentBoundsHandlersAndAcknowledgesAfterResult(t *testing.T) {
	s := &concurrentTestSubscriber{messages: make(chan *Message, 5)}
	started := make(chan *Message, 5)
	release := make(chan struct{})
	consumer, err := ConsumeConcurrent(t.Context(), nil, s, "test.concurrent.counter", 3, func(msg *Message) error {
		started <- msg
		<-release
		if msg.UUID == "failed" {
			return errors.New("SQL rolled back")
		}
		return nil
	}, zap.NewNop())
	require.NoError(t, err)
	messages := []*Message{NewMessage("one", nil), NewMessage("failed", nil), NewMessage("three", nil), NewMessage("four", nil), NewMessage("five", nil)}
	for _, msg := range messages {
		s.messages <- msg
	}
	for range 3 {
		select {
		case msg := <-started:
			select {
			case <-msg.Acked():
				t.Fatal("ACK before handler completion")
			default:
			}
		case <-time.After(time.Second):
			t.Fatal("handlers did not run concurrently")
		}
	}
	select {
	case <-started:
		t.Fatal("exceeded fixed worker count")
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, consumer.Drain(ctx), context.Canceled)
	close(release)
	require.NoError(t, s.Close())
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, consumer.Drain(ctx))
	for _, msg := range messages {
		if msg.UUID == "failed" {
			select {
			case <-msg.Nacked():
			default:
				t.Fatal("failed write was not NACKed")
			}
		} else {
			select {
			case <-msg.Acked():
			default:
				t.Fatal("successful write was not ACKed")
			}
		}
	}
}

func TestConsumeConcurrentRejectsInvalidWorkersAndSubscriptionErrors(t *testing.T) {
	s := &concurrentTestSubscriber{err: errors.New("subscribe failed")}
	_, err := ConsumeConcurrent(t.Context(), nil, s, "test", 0, nil, zap.NewNop())
	require.Error(t, err)
	require.False(t, s.subscribed)
	_, err = ConsumeConcurrent(t.Context(), nil, s, "test", 1, nil, zap.NewNop())
	require.ErrorIs(t, err, s.err)
}
