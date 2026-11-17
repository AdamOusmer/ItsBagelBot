// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"sync"

	"github.com/nats-io/nats.go"
)

type callbackGate struct {
	mu       sync.Mutex
	stopping bool
	stopCh   chan struct{}
	active   sync.WaitGroup
}

type pendingDelivery struct {
	msg   *Message
	watch *resultWatch
}

type subscriptionPump struct {
	queue chan pendingDelivery
	stop  chan struct{}
	once  sync.Once
}

func newSubscriptionPump() *subscriptionPump {
	return &subscriptionPump{
		queue: make(chan pendingDelivery, durableQueueDepth),
		stop:  make(chan struct{}),
	}
}

// halt must run only after callbacks.stopAndWait, or the pump's final drain races an enqueuer.
func (p *subscriptionPump) halt() { p.once.Do(func() { close(p.stop) }) }

func newCallbackGate() *callbackGate {
	return &callbackGate{stopCh: make(chan struct{})}
}

func (g *callbackGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return false
	}
	g.active.Add(1)
	return true
}

func (g *callbackGate) leave() { g.active.Done() }

func (g *callbackGate) stopped() <-chan struct{} { return g.stopCh }

func (g *callbackGate) stopAndWait() {
	g.mu.Lock()
	if !g.stopping {
		g.stopping = true
		close(g.stopCh)
	}
	g.mu.Unlock()
	g.active.Wait()
}

func stopSubscription(sub *nats.Subscription, callbacks *callbackGate, pump *subscriptionPump) {
	_ = sub.Unsubscribe()
	callbacks.stopAndWait()
	pump.halt()
}
