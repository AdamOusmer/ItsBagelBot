// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type pushLane struct {
	*pullSubscriber
	queue   chan jsapi.Msg
	deleted chan struct{}
	fault   atomic.Bool

	mu   sync.Mutex
	sub  *nats.Subscription
	done chan struct{}

	unbound   int
	stalled   int
	delivered uint64
}

func newPushLane(s *pullSubscriber) *pushLane {
	return &pushLane{
		pullSubscriber: s,
		queue:          make(chan jsapi.Msg, s.desired.MaxAckPending),
		deleted:        make(chan struct{}, 1),
	}
}

func (p *pushLane) bind() error {
	if err := p.subscribeQueue(); err != nil {
		return err
	}
	return p.nc.FlushTimeout(time.Second)
}

func (p *pushLane) subscribeQueue() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return nats.ErrBadSubscription
	}
	if p.sub != nil {
		_ = p.sub.Unsubscribe()
	}
	sub, err := p.nc.QueueSubscribe(p.desired.DeliverSubject, p.desired.DeliverGroup, p.receive)
	if err != nil {
		return err
	}
	if err := sub.SetPendingLimits(-1, -1); err != nil {
		return err
	}
	done := make(chan struct{})
	sub.SetClosedHandler(func(string) { close(done) })
	p.sub, p.done = sub, done
	return nil
}

func (p *pushLane) receive(msg *nats.Msg) {
	if status := msg.Header.Get(statusHeader); status != "" && len(msg.Data) == 0 {
		p.control(msg, status)
		return
	}
	wire := pushWire{msg: msg, nc: p.nc}
	p.mu.Lock()
	defer p.mu.Unlock()
	if msg.Sub != p.sub {
		_ = wire.Nak()
		return
	}
	p.trackDelivery(wire)
	select {
	case p.queue <- wire:
	default:
		p.releaseDelivery(wire.Reply())
		_ = wire.NakWithDelay(pullNakDelay())
	}
}

func (p *pushLane) control(msg *nats.Msg, status string) {
	switch {
	case status == controlStatus:
		response := flowControlResponse(msg, flowPosition{})
		if response == nil {
			return
		}
		if err := p.nc.PublishMsg(response); err != nil {
			p.subscriptionError(p.nc, msg.Sub, err)
		}
	case status == "409" && msg.Header.Get(descriptionHeader) == "Consumer Deleted":
		select {
		case p.deleted <- struct{}{}:
		default:
		}
	}
}

func (p *pushLane) subscriptionError(nc *nats.Conn, sub *nats.Subscription, err error) {
	logAsyncError(nc, sub, err)
	p.fault.Store(true)
	p.markLaneError()
}

func (p *pushLane) pump() {
	defer p.workers.Done()
	for wire := range p.queue {
		if !p.deliver(wire) {
			p.returnBatch(p.queue)
			return
		}
	}
}

func (p *pushLane) drain() {
	p.mu.Lock()
	sub, done := p.sub, p.done
	p.mu.Unlock()
	if sub != nil && sub.Drain() == nil {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			_ = sub.Unsubscribe()
		}
	}
	p.mu.Lock()
	p.sub = nil
	close(p.queue)
	p.mu.Unlock()
}

func (p *pushLane) inspect(info *jsapi.ConsumerInfo) error {
	if info.Paused {
		p.unbound, p.stalled = 0, 0
		return nil
	}
	if !p.nc.IsConnected() {
		return nats.ErrDisconnected
	}
	p.unbound = nextStreak(p.unbound, !info.PushBound)
	p.stalled = nextStreak(p.stalled, p.noProgress(info))
	p.delivered = info.Delivered.Stream
	if p.needsSubscription() {
		return p.refreshSubscription(info)
	}
	if p.unbound != 0 || p.stalled != 0 {
		return errors.New("bus: push lane delivery is stalled")
	}
	return nil
}

func (p *pushLane) needsSubscription() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	valid := p.sub != nil && p.sub.IsValid()
	fault := p.fault.Swap(false)
	stalled := p.unbound >= 2 || p.stalled >= 2
	return !valid || fault || stalled
}

func (p *pushLane) refreshSubscription(info *jsapi.ConsumerInfo) error {
	if p.stalled >= 2 {
		p.log.Error("push lane wedged", zap.String("consumer", p.name), zap.Uint64("pending", info.NumPending))
	}
	p.unbound, p.stalled = 0, 0
	if err := p.bind(); err != nil {
		return err
	}
	return errors.New("bus: push lane subscription refreshed")
}

func (p *pushLane) noProgress(info *jsapi.ConsumerInfo) bool {
	return info.NumPending > 0 && info.NumAckPending < p.desired.MaxAckPending && p.delivered == info.Delivered.Stream
}
