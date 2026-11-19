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

func TestConsumeLeavesDeferredFailuresWithTheirOwner(t *testing.T) {
	for _, panicking := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panicking], func(t *testing.T) {
			feed := &feedSubscriber{messages: make(chan *Message, 2)}
			defer close(feed.messages)
			first, sentinel := NewMessage("deferred", nil), NewMessage("sentinel", nil)
			handle := func(msg *Message) error {
				if msg.UUID == "sentinel" {
					return nil
				}
				msg.DeferCompletion()
				if panicking {
					panic("unwinding deferred work")
				}
				return errors.New("deferred work still pending")
			}
			require.NoError(t, Consume(t.Context(), nil, feed, "test.pending", handle, zap.NewNop()))
			feed.messages <- first
			feed.messages <- sentinel
			waitFor(t, func() bool { return signalClosed(sentinel.Acked()) }, "sentinel was not processed")
			require.False(t, signalClosed(first.Nacked()))
			first.DeferCompletion().Complete(context.Canceled)
			require.True(t, signalClosed(first.Nacked()))
		})
	}
}

func TestConsumeQuarantinesDeferredPoison(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler", true: "completion"}[complete], func(t *testing.T) {
			f := completionFixture(t)
			sub := openSubscriber(t, f, "worker")
			transferred := make(chan struct{}, 1)
			require.NoError(t, Consume(t.Context(), nil, sub, hotLane, func(msg *Message) error {
				msg.reject = func(*Message) error { transferred <- struct{}{}; return nil }
				c := msg.DeferCompletion()
				err := Poison(errors.New("invalid deferred input"))
				if complete {
					c.Complete(err)
				}
				return err
			}, zap.NewNop()))
			f.appendMessage(t, hotLane, "poison")
			select {
			case <-transferred:
			case <-time.After(time.Second):
				t.Fatal("deferred poison was not quarantined")
			}
			require.Eventually(t, func() bool { return f.messageCount(TwitchIngressPremiumStream) == 0 }, time.Second, time.Millisecond)
		})
	}
}
