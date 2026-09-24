// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type RPCPool struct {
	policy RPCPoolPolicy
	work   chan rpcJob

	mu       sync.Mutex
	stopping bool
	live     int
	pending  int
	subs     []*nats.Subscription

	senders sync.WaitGroup
	workers sync.WaitGroup

	drainOnce sync.Once
	drained   chan struct{}
}

type rpcJob struct {
	msg     *nats.Msg
	handler nats.MsgHandler
}

var rpcDrainingReply = []byte(`{"error":"service draining"}`)

type RPCPoolPolicy struct {
	MinWorkers  int
	MaxWorkers  int
	QueueDepth  int
	IdleTimeout time.Duration
}

const (
	defaultRPCMinWorkers = 1

	defaultRPCMaxWorkers = 4

	defaultRPCIdleTimeout = 30 * time.Second
)

func (p RPCPoolPolicy) normalized() RPCPoolPolicy {
	if p.MinWorkers < 1 {
		p.MinWorkers = defaultRPCMinWorkers
	}
	if p.MaxWorkers < 1 {
		p.MaxWorkers = defaultRPCMaxWorkers
	}
	if p.MaxWorkers < p.MinWorkers {
		p.MaxWorkers = p.MinWorkers
	}
	if p.QueueDepth == 0 {
		p.QueueDepth = p.MaxWorkers
	}
	if p.QueueDepth < 0 {
		p.QueueDepth = 0
	}
	if p.IdleTimeout <= 0 {
		p.IdleTimeout = defaultRPCIdleTimeout
	}
	return p
}

func newRPCPool(policy RPCPoolPolicy) *RPCPool {
	policy = policy.normalized()
	return &RPCPool{
		policy:  policy,
		work:    make(chan rpcJob, policy.QueueDepth),
		drained: make(chan struct{}),
	}
}

func (p *RPCPool) callback(handler nats.MsgHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		p.submit(rpcJob{msg: msg, handler: handler})
	}
}

func (p *RPCPool) submit(job rpcJob) {
	if !p.admit() {
		respondDraining(job.msg)
		return
	}
	defer p.senders.Done()
	p.work <- job
}

func (p *RPCPool) admit() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return false
	}
	p.senders.Add(1)
	p.pending++
	p.growLocked()
	return true
}

func (p *RPCPool) growLocked() {
	if p.pending <= p.live || p.live >= p.policy.MaxWorkers {
		return
	}
	p.live++
	p.workers.Add(1)
	go p.run()
}

func (p *RPCPool) run() {
	defer p.workers.Done()

	idle := time.NewTimer(p.policy.IdleTimeout)
	defer idle.Stop()

	for {
		select {
		case job, ok := <-p.work:
			if !ok {
				p.dropWorker()
				return
			}
			p.handle(job)
		case <-idle.C:
			if p.retireSelf() {
				return
			}
		}
		idle.Reset(p.policy.IdleTimeout)
	}
}

func (p *RPCPool) retireSelf() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.live <= p.policy.MinWorkers || p.pending >= p.live {
		return false
	}
	p.live--
	return true
}

func (p *RPCPool) dropWorker() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live--
}

func (p *RPCPool) handle(job rpcJob) {
	defer p.complete()
	defer func() {
		if r := recover(); r != nil {
			zap.L().Error("rpc handler panic recovered",
				zap.Any("panic", r), zap.String("subject", job.msg.Subject))
			_ = sendResponse(job.msg, []byte(`{"error":"internal error"}`))
		}
	}()
	job.handler(job.msg)
}

func (p *RPCPool) complete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending--
}

func (p *RPCPool) adopt(subs []*nats.Subscription) {
	p.mu.Lock()
	if !p.stopping {
		p.subs = append(p.subs, subs...)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	unsubscribeAll(subs)
}

func (p *RPCPool) Drain(ctx context.Context) error {
	p.drainOnce.Do(func() { go p.teardown() })
	select {
	case <-p.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *RPCPool) teardown() {
	defer close(p.drained)

	unsubscribeAll(p.closeGate())
	p.senders.Wait()
	// Only after senders.Wait: a late submit would send on the closed channel.
	close(p.work)
	p.workers.Wait()
}

func (p *RPCPool) closeGate() []*nats.Subscription {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopping = true
	subs := p.subs
	p.subs = nil
	return subs
}

func (p *RPCPool) stats() (pending, workers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending, p.live
}

func unsubscribeAll(subs []*nats.Subscription) {
	for _, sub := range subs {
		_ = sub.Unsubscribe()
	}
}

func respondDraining(msg *nats.Msg) {
	_ = msg.Respond(rpcDrainingReply)
}

var rpcPools struct {
	mu    sync.Mutex
	pools []*RPCPool
}

func registerRPCPool(pool *RPCPool) {
	rpcPools.mu.Lock()
	defer rpcPools.mu.Unlock()
	rpcPools.pools = append(rpcPools.pools, pool)
}

// Call before closing the NATS connection or any store the handlers use.
func DrainRPCHandlers(ctx context.Context) error {
	rpcPools.mu.Lock()
	pools := append([]*RPCPool(nil), rpcPools.pools...)
	rpcPools.mu.Unlock()

	var firstErr error
	for _, pool := range pools {
		if err := pool.Drain(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
