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

// RPCPool moves a core-NATS RPC handler off the subscription's delivery
// goroutine, the same constraint concurrentDurableSubscriber already answers on
// the JetStream side: nats.go runs ONE goroutine per async subscription
// (waitForMsgs), pops a message, calls the callback inline, and does not pop the
// next one until that callback has returned.
//
// The cap that causes is not quite one request per pod. QueueSubscribeRPC
// registers the same handler on two subjects — the generic HA subject and
// "<subject>.node.$NODE_NAME" — and each subscription gets its own delivery
// goroutine, so two requests CAN overlap. In production they essentially never
// do: requestLocalFirst sends to the node-local subject first and falls back to
// the generic one only when NATS proves there is no local responder, so
// virtually all traffic lands on one of the two subscriptions and the other sits
// idle. The real defect is head-of-line blocking on whichever subscription is
// actually receiving: one slow handler holds the reader, and every request
// behind it waits for work that has not started.
//
// That is invisible while every handler is a cache hit and brutal the moment one
// is not. gossip's cold lookups go to an upstream API behind a 10s client
// timeout, so a single 3-second miss parked every later request on that subject
// behind it, including the cache hits that would have answered in microseconds.
// The requester's own 5s RPC deadline then expired on work that had never begun,
// and the pod looked healthy throughout because it was doing exactly what it had
// been asked, one at a time.
//
// The pool removes the head of that line: the delivery callback hands the
// message to a persistent worker and returns, so the reader pops the next
// message immediately. Its shape is taken from the sesame consumer fleet in
// weighted.go (persistent workers, a bounded work channel, retirement between
// messages) but it is a deliberately separate implementation — weighted.go
// carries the production event pipeline, its gate/reserve/two-tier-autoscaler
// machinery answers a different question, and nothing here may perturb it.
//
// THE WHOLE HANDLER RUNS ON THE WORKER. The dispatcher touches nothing but the
// *nats.Msg. That is not a stylistic preference: a newrelic.Transaction has
// goroutine affinity, so starting one on the delivery goroutine and finishing it
// on a worker corrupts the trace it is supposed to describe. Decode, transaction,
// handler and respond all belong on the same worker goroutine.
//
// HANDLERS RUN CONCURRENTLY. A handler registered through a pool may be invoked
// on several goroutines at once. A handler that closes over mutable state, or
// that reads-then-writes a shared record, must be made safe before it is moved
// onto a pool — read-modify-write RPCs elsewhere in the fleet are NOT safe today
// and are deliberately left on the serial path.
type RPCPool struct {
	policy RPCPoolPolicy
	work   chan rpcJob

	// mu guards the whole scaling decision. One uncontended lock per message and
	// one per completion is nothing against a handler that talks to Valkey and
	// an upstream API, and it buys an EXACT growth signal instead of a sampled
	// one: an idle-worker hint read outside the lock is stale by construction,
	// because a buffered send completes before the parked worker it was counting
	// on has been scheduled. A whole burst can then observe the same free worker
	// and decline to grow, and since the messages are already in the buffer
	// there is no later message to correct the reading — the burst runs one at a
	// time, which is the bug this file exists to remove.
	mu       sync.Mutex
	stopping bool
	// live is the number of spawned workers that have not returned.
	live int
	// pending is the number of messages admitted and not yet completed —
	// running, buffered, or held by a sender blocked at the ceiling. pending >
	// live is therefore the exact statement "this message has no worker", and
	// pending < live the exact statement "a worker can be spared".
	pending int
	subs    []*nats.Subscription

	// senders counts submits in flight. Drain waits for it before closing the
	// work channel, which is what makes "no send on a closed channel" a
	// structural property rather than a timing bet: admit refuses new submits
	// under the same mutex that sets stopping, so once stopping is set the
	// counter can only fall.
	senders sync.WaitGroup
	workers sync.WaitGroup

	drainOnce sync.Once
	drained   chan struct{}
}

// rpcJob is the unit handed to the fleet: the message and the handler that
// answers it. The handler rides along rather than being looked up by index
// (weighted.go's dispatch carries a lane number) because the field is one word
// either way; the job stays a plain two-word copy into the channel buffer, so
// nothing on the delivery goroutine's serial path escapes to the heap.
type rpcJob struct {
	msg     *nats.Msg
	handler nats.MsgHandler
}

// rpcDrainingReply answers a request that arrives after the pool has stopped
// admitting work. The fleet's RPC contract normalizes a JSON {"error": "..."}
// body into a Go error (see rpcErrorMessage), so the requester fails fast and
// visibly instead of burning its full timeout on a pod that is shutting down.
var rpcDrainingReply = []byte(`{"error":"service draining"}`)

// RPCPoolPolicy sizes one handler fleet. Zero values are replaced with safe
// defaults by normalized(), so RPCPoolPolicy{} is the supported way to ask for
// the default sizing.
type RPCPoolPolicy struct {
	// MinWorkers is the warm floor a worker will not retire below once traffic
	// has created it. Workers are created lazily, so a pool that never receives
	// a message never spawns anything.
	MinWorkers int
	// MaxWorkers is the ceiling on concurrent handlers, and therefore on
	// concurrent handler memory. See defaultRPCMaxWorkers for the arithmetic.
	MaxWorkers int
	// QueueDepth is the work channel's buffer: messages accepted but not yet
	// started. Outstanding messages are bounded by MaxWorkers + QueueDepth. A
	// negative value asks for an unbuffered handoff.
	QueueDepth int
	// IdleTimeout is how long a worker sits idle before retiring itself.
	IdleTimeout time.Duration
}

const (
	// defaultRPCMinWorkers keeps one warm worker per pool once the pool has seen
	// its first message, so a steady trickle of requests never pays a spawn. One
	// parked goroutine is ~8KiB.
	defaultRPCMinWorkers = 1

	// defaultRPCMaxWorkers is MODEST CONCURRENCY, NOT UNLIMITED, and it is
	// chosen against memory rather than against CPU.
	//
	// The I/O-bound argument says the ceiling could be large: these handlers sit
	// on upstream sockets and Valkey round trips, not on cores, and the
	// per-upstream token buckets in app/gossip/internal/core/buckets.go already
	// bound what the concurrency can actually spend upstream. Memory says
	// otherwise, and memory is the binding constraint. gossip runs with requests
	// 10m CPU / 32Mi, limits 250m CPU / 128Mi and GOMEMLIMIT=96MiB, and
	// app/gossip/internal/core/http.go reads each upstream response into a single
	// buffer bounded by maxBody = 4MiB, then codec.Unmarshal allocates the
	// decoded value on top of that still-live buffer.
	//
	// Only a RUNNING handler holds that memory. A message waiting in the work
	// channel is just the inbound NATS request — an account name and a platform,
	// well under a KiB — so QueueDepth costs kilobytes and the bound that matters
	// is workers x maxBody:
	//
	//	4 x 4MiB = 16MiB of body buffers on a saturated subject, ~32MiB at the
	//	decode peak where the buffer and the decoded value are both live. On top
	//	of gossip's 16-18MiB live baseline that is ~50MiB: well under
	//	GOMEMLIMIT=96MiB and far from the 128Mi container limit that would mean
	//	an OOMKill, and it leaves room for a second subject to be hot at the same
	//	time (~82MiB, still under GOMEMLIMIT).
	//
	//	8 would put a single saturated subject at ~64MiB of peak and a second one
	//	through the container limit. An OOMKilled gossip pod is strictly worse
	//	than a slow one, so the ceiling stops where the budget stops.
	//
	// Pools are per subscription, so the process-wide worst case is this ceiling
	// times the number of subjects. gossip registers nineteen: urchin 5,
	// fortnite 4, clashroyale 4, mcsr 3, govee 2, hypixel 1. All nineteen
	// saturated with
	// pathological 4MiB bodies is past the limit and is NOT defended by this
	// ceiling; it is defended by the shape of the workload (chat commands, one or
	// two hot endpoints at a time), by the per-upstream buckets, and by the fact
	// that 4MiB is the guard against a misbehaving upstream while the largest
	// real payload — a full Hypixel profile — is a few hundred KiB, which puts
	// even all fifteen saturated well inside the budget. If that ever stops being
	// true the answer is a process-wide admission gate or a lower ceiling, not a
	// bigger container.
	//
	// And 4 is enough to fix the bug. The defect is head-of-line blocking behind
	// ONE slow handler; four means a 3-second cold lookup no longer stalls the
	// three cache hits behind it. Reaching for a bigger number buys throughput
	// this pod has neither the CPU quota nor the heap to use.
	defaultRPCMaxWorkers = 4

	// defaultRPCIdleTimeout matches ScalePolicy's ScaleDownAfter: long enough
	// that a burst does not thrash goroutines, short enough that a pod idle
	// between streams gives the stacks back.
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
	// Unset means "match the ceiling"; negative is an explicit request for an
	// unbuffered handoff, which bounds outstanding messages on workers alone.
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

// callback wraps handler in the nats.go callback the subscription registers. It
// is the whole fix in three lines: build the job, hand it over, return. Whatever
// the handler costs is now paid on a worker, not on the subscription's single
// reader goroutine.
func (p *RPCPool) callback(handler nats.MsgHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		p.submit(rpcJob{msg: msg, handler: handler})
	}
}

// submit hands one message to the fleet.
//
// The send BLOCKS once MaxWorkers handlers are running and QueueDepth messages
// are already waiting, and blocking there is the correct behavior. The
// alternatives are worse: dropping a request silently turns a slow pod into a
// requester-visible timeout with no signal, and an unbounded queue turns it into
// the OOMKill the ceiling exists to prevent. Blocking pushes the backlog into
// nats.go's own pending buffer, where it is visible as a slow consumer, and it
// is what keeps "outstanding messages <= MaxWorkers + QueueDepth" true.
//
// It is also strictly better than what it replaces. The old behavior blocked the
// reader on EVERY message, because the callback ran the handler; this one blocks
// only when the pod is genuinely at its concurrency budget, and until then the
// reader returns in the time it takes to copy two words into a channel.
func (p *RPCPool) submit(job rpcJob) {
	if !p.admit() {
		respondDraining(job.msg)
		return
	}
	defer p.senders.Done()
	p.work <- job
}

// admit accounts for one message and grows the fleet if that message has no
// worker, or reports that the pool has stopped accepting.
//
// The stopping flag and the sender count move under the same mutex for the
// reason callbackGate exists in concurrent_subscriber.go: nats.go can invoke a
// callback just after Unsubscribe returns, so a bare WaitGroup would let an Add
// race Drain's Wait.
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

// growLocked adds one worker when the message just admitted has none to run it.
// That is the whole scaling rule: demand-driven growth to the ceiling, idle
// retirement back to the floor. weighted.go's timed supervisor is not warranted
// here because there is nothing to pace — an RPC handler holds no redelivery
// budget and no lane reserve, so a worker spawned for one message and retired
// 30s later costs a goroutine, not a correctness property. A burst of n
// simultaneous requests therefore arrives at exactly min(n, MaxWorkers) workers,
// one per admission, with no ramp.
func (p *RPCPool) growLocked() {
	if p.pending <= p.live || p.live >= p.policy.MaxWorkers {
		return
	}
	p.live++
	p.workers.Add(1)
	go p.run()
}

// run is one persistent worker. It leaves the loop only BETWEEN messages —
// retiring on an idle timeout it can only observe while parked, or exiting when
// the work channel is closed and drained — so no shrink and no shutdown can ever
// kill a handler mid-message.
func (p *RPCPool) run() {
	defer p.workers.Done()

	// Go 1.23 timer semantics (this module declares go 1.26.5): Reset on a timer
	// that has already fired guarantees no stale tick is received afterwards, so
	// the loop needs no drain dance around the reset.
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

// retireSelf reports whether this worker may exit, and books its departure in
// the same critical section as the decision so an admission cannot size the
// fleet against a worker that is already leaving. It keeps MinWorkers warm and
// refuses while pending >= live, so retiring can never leave a message in the
// buffer with no worker to take it. The caller is parked between messages by
// construction, so a true here cannot abandon work in progress either.
func (p *RPCPool) retireSelf() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.live <= p.policy.MinWorkers || p.pending >= p.live {
		return false
	}
	p.live--
	return true
}

// dropWorker books the departure of a worker leaving because the pool is closed,
// which is the one exit retireSelf does not decide.
func (p *RPCPool) dropWorker() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live--
}

// handle runs one job. Recovery used to be skipped because it would leave the
// requester waiting on a reply nobody was going to send; the answer is to send
// that reply from the recovery path. Without this, one persisted bad handler
// input (sesame's {random} int64 overflow) crash-looped whole pods fleet-wide
// until someone purged database rows by hand.
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

// adopt takes ownership of a subscription so Drain can silence it before it
// waits. A pool already draining unsubscribes immediately instead, so a
// registration that loses the race to shutdown cannot leave a live subscription
// pointed at a fleet that will never run it.
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

// Drain stops admitting requests and waits for every handler already dispatched
// to finish, or until ctx is done, whichever comes first.
//
// The order is the contract, and it is the same one concurrent_subscriber.Close
// follows: unsubscribe first so nats.go stops delivering, close the admission
// gate, wait for in-flight submits so nothing can send, only then close the work
// channel and wait for the fleet to run the buffer down. A caller that returns
// from Drain therefore knows no handler is still touching a database handle, a
// Valkey client or a NATS connection it is about to close.
//
// Drain is idempotent and safe to call from several goroutines: the teardown
// runs once, off the caller's goroutine, so a ctx deadline bounds the WAIT
// without abandoning the work. It returns ctx.Err() if the deadline wins, in
// which case some handlers are still running and their requesters will time out.
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
	// Every sender holds a token taken under the same mutex that set stopping,
	// so this Wait converges and, once it returns, no goroutine can be on its
	// way to the channel.
	p.senders.Wait()
	close(p.work)
	p.workers.Wait()
}

// closeGate flips the pool to draining and yields the subscriptions to silence.
func (p *RPCPool) closeGate() []*nats.Subscription {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopping = true
	subs := p.subs
	p.subs = nil
	return subs
}

// stats reports the messages admitted but not yet completed (running, buffered,
// or held by a sender blocked at the ceiling) and the number of live workers.
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

// rpcPools is every pool a successful QueueSubscribeRPCConcurrent has built. A
// package-level registry is what lets a service drain handlers it never holds a
// handle to: the pools are created one per subscription, deep inside whatever
// registers the endpoints, and a service that forgets to drain is a service that
// closes its database while a handler is mid-query. One DrainRPCHandlers call
// covers all of them.
var rpcPools struct {
	mu    sync.Mutex
	pools []*RPCPool
}

func registerRPCPool(pool *RPCPool) {
	rpcPools.mu.Lock()
	defer rpcPools.mu.Unlock()
	rpcPools.pools = append(rpcPools.pools, pool)
}

// DrainRPCHandlers drains every pool created by QueueSubscribeRPCConcurrent,
// sharing ctx as the deadline for the whole set. Call it at shutdown after the
// process's own context is cancelled and BEFORE closing the NATS connection and
// any store the handlers use; the deferred closes in main then run against a
// surface with nothing left in flight.
//
// It reports the first pool that did not finish before ctx expired. Draining an
// already-drained pool is a no-op, so calling it twice is harmless.
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
