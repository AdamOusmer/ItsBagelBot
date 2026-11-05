// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"go.uber.org/zap"
)

const (
	maxQueueWaiting         = 100
	runQueueCallTimeout     = 2 * time.Second
	runQueueCloseTimeout    = 3 * time.Second
	queueReasonFull         = "queue_full"
	queueReasonTimeout      = "queue_timeout"
	queueReasonCanceled     = "canceled"
	queueReasonGranted      = "granted"
	queueReasonValkeyAbsent = "valkey_unavailable"
	queueReasonHandedOff    = "handed_off"
)

// RunQueueOut means the queue is full on Enter, or the seat is gone on Touch.
const RunQueueOut int64 = -1

type RunQueueSeat struct {
	BroadcasterID uint64
	Queue         string
	Ticket        string
}

// RunQueue answers with the seat's position: 0 holds the slot, above 0 waits, RunQueueOut is refused.
type RunQueue interface {
	Enter(ctx context.Context, seat RunQueueSeat) (int64, error)
	Touch(ctx context.Context, seat RunQueueSeat) (int64, error)
	Leave(ctx context.Context, seat RunQueueSeat) error
}

type openRunQueue struct{}

func (openRunQueue) Enter(context.Context, RunQueueSeat) (int64, error) { return 0, nil }

func (openRunQueue) Touch(context.Context, RunQueueSeat) (int64, error) { return 0, nil }

func (openRunQueue) Leave(context.Context, RunQueueSeat) error { return nil }

type queueTiming struct {
	maxWait     time.Duration
	renew       time.Duration
	pollStep    time.Duration
	pollMax     time.Duration
	unavailable time.Duration
}

var defaultQueueTiming = queueTiming{
	maxWait:     5 * time.Minute,
	renew:       runQueueLease / 3,
	pollStep:    100 * time.Millisecond,
	pollMax:     time.Second,
	unavailable: runQueueLease,
}

func (t queueTiming) pollAfter(position int64) time.Duration {
	return min(time.Duration(position)*t.pollStep, t.pollMax)
}

type queueSlot struct {
	seat  RunQueueSeat
	refs  atomic.Int32
	ended chan struct{}
}

func (s *queueSlot) retain() {
	if s != nil {
		s.refs.Add(1)
	}
}

func (s *queueSlot) drop() {
	if s != nil && s.refs.Add(-1) == 0 {
		close(s.ended)
	}
}

type queueWaiters struct {
	mu      sync.Mutex
	count   int
	closed  bool
	wg      sync.WaitGroup
	closing context.Context
	cancel  context.CancelFunc
}

func newQueueWaiters() *queueWaiters {
	closing, cancel := context.WithCancel(context.Background())
	return &queueWaiters{closing: closing, cancel: cancel}
}

func (w *queueWaiters) enter() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return false
	}
	w.count++
	w.wg.Add(1)
	return true
}

func (w *queueWaiters) done() {
	w.mu.Lock()
	w.count--
	w.mu.Unlock()
	w.wg.Done()
}

func (w *queueWaiters) close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.cancel()
	drained := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(drained)
	}()
	timer := time.NewTimer(runQueueCloseTimeout)
	defer timer.Stop()
	select {
	case <-drained:
	case <-timer.C:
	}
}

// Valkey errors run the recipe unqueued, matching automationRunAllowed: an outage must not stop every recipe.
func (p *Pipeline) admitToQueue(ctx context.Context, run *AutomationRun) bool {
	queue := run.Plan.Automation.Definition.Queue
	// A child of a slot holder joins that slot: waiting on any queue here could deadlock the parent.
	if queue == "" || run.Slot != nil {
		return true
	}
	seat := RunQueueSeat{BroadcasterID: run.BroadcasterID, Queue: queue, Ticket: run.ID}
	callCtx, cancel := context.WithTimeout(ctx, runQueueCallTimeout)
	defer cancel()
	position, err := p.automations.Queue.Enter(callCtx, seat)
	switch {
	case err != nil:
		p.log.Warn("automations: run queue unavailable, running unqueued", zap.String("run_id", run.ID), zap.Error(err))
		return true
	case position == RunQueueOut:
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, queueReasonFull
		return false
	case position == 0:
		run.Slot, run.OwnsSlot = p.holdSlot(seat), true
		return true
	}
	p.startQueueWait(ctx, run, queueTurn{seat: seat, position: position, until: time.Now().Add(p.queueTiming.maxWait)})
	return false
}

type queueTurn struct {
	seat     RunQueueSeat
	position int64
	until    time.Time
}

func (p *Pipeline) startQueueWait(ctx context.Context, run *AutomationRun, turn queueTurn) {
	if !p.queueWaiters.enter() {
		p.leaveQueue(turn.seat)
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, queueReasonFull
		return
	}
	// The handler returns now and recycles the run's pooled envelope and args, so the waiter gets its own copy.
	waiting := detachRun(run)
	run.Record.Outcome = outcomeDeferred
	if !p.durable.recordWaiter(ctx, waiting, turn) {
		p.leaveQueue(turn.seat)
		p.queueWaiters.done()
		return
	}
	go p.awaitQueueTurn(context.WithoutCancel(ctx), waiting, turn)
}

func (p *Pipeline) awaitQueueTurn(ctx context.Context, run *AutomationRun, turn queueTurn) {
	if p.finishQueueWait(ctx, run, turn) {
		p.runQueuedTurn(ctx, run)
	}
}

func (p *Pipeline) finishQueueWait(ctx context.Context, run *AutomationRun, turn queueTurn) bool {
	defer p.queueWaiters.done()
	reason := p.waitQueueTurn(ctx, run, turn)
	switch reason {
	case queueReasonGranted:
		run.Slot = p.holdSlot(turn.seat)
		return true
	case queueReasonValkeyAbsent:
		p.log.Warn("automations: run queue unavailable, running unqueued", zap.String("run_id", run.ID))
		return true
	case queueReasonHandedOff:
		return false
	case queueReasonCanceled:
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeFailed, reason
	default:
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, reason
	}
	p.finishRun(ctx, run)
	p.leaveQueue(turn.seat)
	return false
}

func (p *Pipeline) waitQueueTurn(ctx context.Context, run *AutomationRun, turn queueTurn) string {
	waitCtx, cancel := context.WithDeadline(ctx, turn.until)
	defer cancel()
	stop := context.AfterFunc(p.queueWaiters.closing, cancel)
	defer stop()
	stopHalt := p.durable.onHalt(cancel)
	defer stopHalt()
	wait := queueWait{p: p, seat: turn.seat, position: turn.position, healthyAt: time.Now()}
	for {
		if !sleepContext(waitCtx, p.queueTiming.pollAfter(wait.position)) {
			return p.queueWaitEnd(waitCtx, run)
		}
		if reason, settled := wait.check(waitCtx); settled {
			return reason
		}
	}
}

func (p *Pipeline) queueWaitEnd(ctx context.Context, run *AutomationRun) string {
	if p.durable.handingOff(run) {
		return queueReasonHandedOff
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return queueReasonTimeout
	}
	return queueReasonCanceled
}

type queueWait struct {
	p         *Pipeline
	seat      RunQueueSeat
	position  int64
	healthyAt time.Time
}

func (w *queueWait) check(ctx context.Context) (string, bool) {
	position, err := w.p.queueStanding(ctx, w.seat)
	if err != nil {
		return queueReasonValkeyAbsent, time.Since(w.healthyAt) >= w.p.queueTiming.unavailable
	}
	w.healthyAt = time.Now()
	w.position = position
	switch position {
	case 0:
		return queueReasonGranted, true
	case RunQueueOut:
		return queueReasonFull, true
	}
	return "", false
}

// A seat whose lease lapsed during an outage rejoins at the back rather than failing the run.
func (p *Pipeline) queueStanding(ctx context.Context, seat RunQueueSeat) (int64, error) {
	callCtx, cancel := context.WithTimeout(ctx, runQueueCallTimeout)
	defer cancel()
	position, err := p.automations.Queue.Touch(callCtx, seat)
	if err != nil || position != RunQueueOut {
		return position, err
	}
	return p.automations.Queue.Enter(callCtx, seat)
}

func (p *Pipeline) runQueuedTurn(parent context.Context, run *AutomationRun) {
	defer run.Slot.drop()
	ctx, cancel := context.WithTimeout(parent, continuationTimeout)
	defer cancel()
	if !p.durable.stillOurs(ctx, run) {
		return
	}
	p.autoExec.segments.Add(1)
	defer p.autoExec.segments.Add(-1)
	defer p.failPanickedSegment(ctx, run)
	if p.automationsLockedFor(ctx, run.BroadcasterID) {
		run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, "premium_required"
		p.finishRun(ctx, run)
		return
	}
	out := p.detachedEmit(ctx, run.BroadcasterID, run.Locale, run.replayKey()+":0")
	x := p.newActionExec(ctx, run, out.emit)
	defer releaseActionExec(x)
	x.execute()
	if err := p.flushDetached(ctx, out); err != nil {
		p.log.Warn("automation queued run publish failed", zap.String("run_id", run.ID), zap.Error(err))
	}
	p.finishRun(ctx, run)
}

func (p *Pipeline) holdSlot(seat RunQueueSeat) *queueSlot {
	slot := &queueSlot{seat: seat, ended: make(chan struct{})}
	slot.refs.Store(1)
	go p.keepSlot(seat, slot)
	return slot
}

func (p *Pipeline) keepSlot(seat RunQueueSeat, slot *queueSlot) {
	ticker := time.NewTicker(p.queueTiming.renew)
	defer ticker.Stop()
	for {
		select {
		case <-slot.ended:
			p.leaveQueue(seat)
			return
		case <-ticker.C:
			p.renewSlot(seat)
		}
	}
}

func (p *Pipeline) renewSlot(seat RunQueueSeat) {
	ctx, cancel := context.WithTimeout(context.Background(), runQueueCallTimeout)
	defer cancel()
	position, err := p.automations.Queue.Touch(ctx, seat)
	if err != nil || position == RunQueueOut {
		p.log.Warn("automations: run queue lease not renewed", zap.String("run_id", seat.Ticket), zap.Int64("position", position), zap.Error(err))
	}
}

func (p *Pipeline) leaveQueue(seat RunQueueSeat) {
	ctx, cancel := context.WithTimeout(context.Background(), runQueueCallTimeout)
	defer cancel()
	if err := p.automations.Queue.Leave(ctx, seat); err != nil {
		p.log.Warn("automations: run queue leave failed, lease will expire", zap.String("run_id", seat.Ticket), zap.Error(err))
	}
}
