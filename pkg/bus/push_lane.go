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
	fault  atomic.Bool
	active atomic.Int64

	mu        sync.Mutex
	ready     *sync.Cond
	sub       *nats.Subscription
	done      chan struct{}
	interrupt chan struct{}
	shedding  bool
	queue     []pushQueued
	head      pushQueued
	observed  *nats.Msg

	unbound   int
	stalled   int
	delivered uint64
}

type pushQueued struct {
	wire    pushWire
	arrival time.Time
}

func newPushLane(s *pullSubscriber) *pushLane {
	p := &pushLane{pullSubscriber: s, interrupt: make(chan struct{})}
	p.ready = sync.NewCond(&p.mu)
	return p
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
	if msg.Sub != p.sub || p.shedding {
		_ = wire.Nak()
		return
	}
	p.trackDelivery(wire)
	p.queue = append(p.queue, pushQueued{wire: wire, arrival: time.Now()})
	p.ready.Signal()
}

func (p *pushLane) control(msg *nats.Msg, status string) {
	if status != controlStatus {
		return
	}
	response := flowControlResponse(msg, flowPosition{})
	if response == nil {
		return
	}
	if err := p.nc.PublishMsg(response); err != nil {
		p.subscriptionError(p.nc, msg.Sub, err)
	}
}

func (p *pushLane) subscriptionError(nc *nats.Conn, sub *nats.Subscription, err error) {
	logAsyncError(nc, sub, err)
	p.fault.Store(true)
	p.markLaneError()
}

func (p *pushLane) pump() {
	defer p.workers.Done()
	for {
		delivery, interrupt := p.nextDelivery()
		if delivery.wire.msg == nil {
			return
		}
		p.deliver(delivery.wire, interrupt)
	}
}

func (p *pushLane) nextDelivery() (pushQueued, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.head = pushQueued{}
	for len(p.queue) == 0 && !p.closed.Load() {
		p.ready.Wait()
	}
	if p.closed.Load() {
		return p.head, nil
	}
	p.head = p.queue[0]
	p.queue[0] = pushQueued{}
	p.queue = p.queue[1:]
	return p.head, p.interrupt
}

func (p *pushLane) drain() {
	p.mu.Lock()
	sub, done := p.sub, p.done
	if sub != nil && !p.shedding {
		_ = sub.Drain()
	}
	p.mu.Unlock()
	if sub != nil {
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
	queued := p.takeQueue()
	p.ready.Broadcast()
	p.mu.Unlock()
	p.returnQueue(queued)
}

func (p *pushLane) takeQueue() []pushQueued {
	queued := p.queue
	p.queue = nil
	return queued
}

func (p *pushLane) returnQueue(queued []pushQueued) {
	for _, delivery := range queued {
		p.returnDelivery(delivery.wire)
	}
}

func (p *pushLane) inspectQueue() error {
	p.mu.Lock()
	if p.shedding {
		resumed := p.resumeQueue()
		p.mu.Unlock()
		if resumed {
			return nil
		}
		return errors.New("bus: push lane pipeline is draining")
	}
	if !p.queueStalled() {
		p.mu.Unlock()
		return nil
	}
	_ = p.sub.Drain()
	p.shedding = true
	close(p.interrupt)
	queued := p.takeQueue()
	p.mu.Unlock()
	p.returnQueue(queued)
	return errors.New("bus: push lane shed a stalled pipeline")
}

func (p *pushLane) resumeQueue() bool {
	select {
	case <-p.done:
	default:
		return false
	}
	if p.head.wire.msg != nil || p.active.Load() != 0 {
		return false
	}
	p.sub, p.observed = nil, nil
	p.interrupt = make(chan struct{})
	p.shedding = false
	return true
}

func (p *pushLane) queueStalled() bool {
	oldest := p.head
	if oldest.wire.msg == nil && len(p.queue) > 0 {
		oldest = p.queue[0]
	}
	unchanged := oldest.wire.msg == p.observed
	p.observed = oldest.wire.msg
	stalled := unchanged || time.Since(oldest.arrival) > p.desired.AckWait/3
	return oldest.wire.msg != nil && stalled
}

func (p *pushLane) inspect(info *jsapi.ConsumerInfo) error {
	if err := p.inspectQueue(); err != nil {
		return err
	}
	if info.Paused {
		p.unbound, p.stalled = 0, 0
		return nil
	}
	if !p.nc.IsConnected() {
		return nats.ErrDisconnected
	}
	p.unbound = nextStreak(p.unbound, !info.PushBound)
	p.stalled = nextStreak(p.stalled, p.noProgress(info))
	p.delivered = info.Delivered.Consumer
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
	invalid := p.fault.Swap(false) || !valid
	stalled := p.unbound >= 2 || p.stalled >= 2
	return invalid || stalled
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
	return info.NumPending > 0 && p.delivered == info.Delivered.Consumer
}
