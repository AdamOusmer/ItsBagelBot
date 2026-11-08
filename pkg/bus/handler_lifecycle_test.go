// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func lifecycleWatch(t *testing.T) (*resultWatch, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	close(closed)
	sub := &concurrentDurableSubscriber{closeCh: closed}
	sub.acks.Add(1)
	return &resultWatch{s: sub, msg: NewMessage("test", nil), cancel: cancel}, ctx
}

func TestResultWatchCancelsOnResolutionAndUnwind(t *testing.T) {
	for name, action := range map[string]func(*resultWatch){
		"ack":    func(w *resultWatch) { w.resolve(true) },
		"nack":   func(w *resultWatch) { w.resolve(false) },
		"unwind": func(w *resultWatch) { w.unwind() },
		"expiry": func(w *resultWatch) { (&keepAliveWheel{steps: 1}).sweepDue(1, w) },
	} {
		t.Run(name, func(t *testing.T) {
			w, ctx := lifecycleWatch(t)
			action(w)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			w.s.acks.Wait()
		})
	}
}

func TestResultWatchConcurrentFinishCancelsOnce(t *testing.T) {
	for range 100 {
		w, ctx := lifecycleWatch(t)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); w.resolve(true) }()
		go func() { defer wg.Done(); w.unwind() }()
		go func() { defer wg.Done(); (&keepAliveWheel{steps: 1}).sweepDue(1, w) }()
		wg.Wait()
		w.s.acks.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	}
}

func TestExpiredDeliveryDoesNotStartHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := NewMessage("expired", nil)
	msg.SetContext(ctx)
	called := false
	lane := consumeLane{handle: func(*Message) error { called = true; return nil }}
	lane.process(msg)
	require.False(t, called)
	select {
	case <-msg.Nacked():
	case <-time.After(time.Second):
		t.Fatal("expired message was not nacked")
	}
}

func TestQueuedDeliveryDeadlineNacksWithoutHandoff(t *testing.T) {
	w, ctx := lifecycleWatch(t)
	expired, cancel := context.WithCancel(ctx)
	cancel()
	w.msg.SetContext(expired)
	w.msg.setResolveHandler(w.resolve)
	output := make(chan *Message)
	require.True(t, w.s.deliver(context.Background(), output, newCallbackGate(), pendingDelivery{msg: w.msg, watch: w}))
	select {
	case <-w.msg.Nacked():
	default:
		t.Fatal("expired queued message was not nacked")
	}
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestBlockedDeliveryStopsOnShutdown(t *testing.T) {
	w, ctx := lifecycleWatch(t)
	w.msg.SetContext(ctx)
	output := make(chan *Message)
	callbacks := newCallbackGate()
	callbacks.stopAndWait()
	require.False(t, w.s.deliver(context.Background(), output, callbacks, pendingDelivery{msg: w.msg, watch: w}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	w.s.acks.Wait()
}
