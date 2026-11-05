// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/utils"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

const (
	durableLease         = 15 * time.Second
	durableClaimBatch    = 256
	durableReasonChanged = "recipe_changed"
)

// DurableStanding answers who holds a record: the caller, nobody because the store lost it, or somebody else.
type DurableStanding int64

const (
	DurableLost    DurableStanding = -1
	DurableMissing DurableStanding = 0
	DurableOwned   DurableStanding = 1
)

type DurableRef struct {
	BroadcasterID uint64
	ID            string
}

type DurableSave struct {
	Ref   DurableRef
	Body  []byte
	Seat  RunQueueSeat
	Fresh bool
}

type DurableClaim struct {
	Ref  DurableRef
	Body []byte
}

// DurableStore keeps paused runs for a successor. Save with Fresh refuses an existing record, so a redelivered event
// cannot start a second copy. The lease belongs to the replica, so renewing costs the same with one paused run or a million.
type DurableStore interface {
	Save(ctx context.Context, owner string, save DurableSave) (DurableStanding, error)
	Standing(ctx context.Context, owner string, ref DurableRef) (DurableStanding, error)
	Finish(ctx context.Context, owner string, ref DurableRef, ticket string) (DurableStanding, error)
	Renew(ctx context.Context, owner string, lease time.Duration) error
	Release(ctx context.Context, owner string) error
	Lapsed(ctx context.Context) (lapsed []string, nextDue time.Duration, err error)
	Claim(ctx context.Context, from, to string, limit int) (claims []DurableClaim, empty bool, err error)
	Wakes(ctx context.Context, wake func()) error
}

type durableTiming struct {
	lease     time.Duration
	renew     time.Duration
	sweepMin  time.Duration
	call      time.Duration
	closeWait time.Duration
}

var defaultDurableTiming = durableTiming{
	lease:     durableLease,
	renew:     durableLease / 3,
	sweepMin:  time.Second,
	call:      runQueueCallTimeout,
	closeWait: runQueueCloseTimeout,
}

type durableTicket struct {
	ref    DurableRef
	ticket string
}

type unfinishedRun struct {
	ticket durableTicket
	seat   RunQueueSeat
}

type durableRuns struct {
	p      *Pipeline
	store  DurableStore
	owner  string
	timing durableTiming
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}

	mu         sync.Mutex
	unfinished []unfinishedRun
}

func newDurableRuns(p *Pipeline, deps AutomationDeps) *durableRuns {
	if deps.Durable == nil {
		return nil
	}
	id, err := utils.NewID()
	if err != nil {
		p.log.Warn("automations: no replica id, paused runs stay in memory", zap.Error(err))
		return nil
	}
	timing := deps.durableTiming
	if timing.lease == 0 {
		timing = defaultDurableTiming
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &durableRuns{p: p, store: deps.Durable, owner: id.String(), timing: timing, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	go d.heartbeat()
	go d.sweepLoop()
	go d.listen()
	return d
}

func (d *durableRuns) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), d.timing.call)
}

// halt stops this replica's durable work where it stands, writing nothing: the same state a crash leaves.
func (d *durableRuns) halt() {
	if d != nil {
		d.cancel()
	}
}

func (d *durableRuns) handOff() {
	if d == nil {
		return
	}
	d.awaitSegments()
	d.retryUnfinished(context.Background())
	d.release(context.Background())
}

func (d *durableRuns) awaitSegments() {
	deadline := time.Now().Add(d.timing.closeWait)
	for d.p.autoExec.segments.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

func (d *durableRuns) release(ctx context.Context) {
	callCtx, cancel := d.callCtx(ctx)
	defer cancel()
	if err := d.store.Release(callCtx, d.owner); err != nil {
		d.p.log.Warn("automations: durable lease not dropped, it will lapse", zap.Error(err))
	}
}

func (d *durableRuns) haltedFor(run *AutomationRun) <-chan struct{} {
	if d == nil || run.Durable == nil {
		return nil
	}
	return d.ctx.Done()
}

func (d *durableRuns) handingOff(run *AutomationRun) bool {
	return d != nil && run.Durable != nil && d.ctx.Err() != nil
}

func (d *durableRuns) onHalt(do func()) (stop func() bool) {
	if d == nil {
		return func() bool { return false }
	}
	return context.AfterFunc(d.ctx, do)
}

func (d *durableRuns) heartbeat() {
	ticker := time.NewTicker(d.timing.renew)
	defer ticker.Stop()
	for {
		callCtx, cancel := d.callCtx(d.ctx)
		if err := d.store.Renew(callCtx, d.owner, d.timing.lease); err != nil && d.ctx.Err() == nil {
			d.p.log.Warn("automations: durable lease not renewed", zap.Error(err))
		}
		cancel()
		d.retryUnfinished(d.ctx)
		select {
		case <-ticker.C:
		case <-d.ctx.Done():
			return
		}
	}
}

func (d *durableRuns) listen() {
	for d.ctx.Err() == nil {
		err := d.store.Wakes(d.ctx, d.nudge)
		if err != nil && d.ctx.Err() == nil {
			d.p.log.Warn("automations: durable wake channel dropped", zap.Error(err))
		}
		sleepContext(d.ctx, d.timing.renew)
	}
}

func (d *durableRuns) nudge() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *durableRuns) sweepLoop() {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-timer.C:
		case <-d.wake:
		}
		timer.Reset(d.sweep())
	}
}

func (d *durableRuns) sweep() time.Duration {
	callCtx, cancel := d.callCtx(d.ctx)
	lapsed, nextDue, err := d.store.Lapsed(callCtx)
	cancel()
	if err != nil {
		return d.timing.renew
	}
	for _, replica := range lapsed {
		if replica != d.owner {
			d.takeFrom(replica)
		}
	}
	return d.sleepFor(nextDue)
}

func (d *durableRuns) sleepFor(nextDue time.Duration) time.Duration {
	if nextDue < 0 {
		return d.timing.lease
	}
	return max(min(nextDue, d.timing.lease), d.timing.sweepMin)
}

func (d *durableRuns) takeFrom(replica string) {
	for d.ctx.Err() == nil {
		callCtx, cancel := d.callCtx(d.ctx)
		claims, empty, err := d.store.Claim(callCtx, replica, d.owner, durableClaimBatch)
		cancel()
		for _, claim := range claims {
			go d.resume(claim)
		}
		if err != nil {
			d.p.log.Warn("automations: durable claim failed", zap.Error(err))
			return
		}
		if empty {
			return
		}
	}
}

func (d *durableRuns) resume(claim DurableClaim) {
	var rec durableRecord
	if err := codec.Unmarshal(claim.Body, &rec); err != nil {
		d.p.log.Warn("automations: dropping unreadable paused run", zap.String("id", claim.Ref.ID), zap.Error(err))
		d.discard(claim.Ref)
		return
	}
	run := rec.run(claim.Ref)
	plan, loaded := d.loadPlan(run.BroadcasterID, rec.AutomationID)
	if !loaded {
		return
	}
	frames, valid := rec.frames(plan)
	if !valid {
		d.finishChanged(run, rec.Seat)
		return
	}
	run.Plan = plan
	d.restart(run, &rec, frames)
}

func (d *durableRuns) discard(ref DurableRef) {
	callCtx, cancel := d.callCtx(d.ctx)
	defer cancel()
	if _, err := d.store.Finish(callCtx, d.owner, ref, ""); err != nil {
		d.p.log.Warn("automations: unreadable paused run not dropped", zap.Error(err))
	}
}

// A store outage must not read as a deleted recipe, so this waits for an answer or for this replica to stop.
func (d *durableRuns) loadPlan(broadcasterID uint64, automationID string) (*automation.Plan, bool) {
	src := d.p.automations.Source
	if src == nil {
		return nil, true
	}
	for d.ctx.Err() == nil {
		index, err := src.AutomationIndex(d.ctx, broadcasterID)
		if err == nil {
			return index.Plan(automationID), true
		}
		d.p.log.Warn("automations: paused run waits for its recipe", zap.Error(err))
		sleepContext(d.ctx, d.timing.renew)
	}
	return nil, false
}

func (d *durableRuns) finishChanged(run *AutomationRun, seat *RunQueueSeat) {
	if seat != nil {
		run.Slot = d.p.holdSlot(*seat)
		defer run.Slot.drop()
	}
	run.Record.Outcome, run.Record.Reason = automationrpc.OutcomeLimited, durableReasonChanged
	d.p.finishRun(d.ctx, run)
}

func (d *durableRuns) restart(run *AutomationRun, rec *durableRecord, frames []resume) {
	p := d.p
	if rec.Waiting {
		if p.queueWaiters.enter() {
			p.awaitQueueTurn(context.Background(), run, queueTurn{seat: *rec.Seat, position: 1, until: time.UnixMilli(rec.WaitUntil)})
		}
		return
	}
	if rec.Seat != nil {
		run.Slot = p.holdSlot(*rec.Seat)
	}
	p.autoExec.running.Add(1)
	wait := max(time.Until(time.UnixMilli(rec.ResumeAt)), 0)
	p.continueRun(context.Background(), continuation{run: run, budget: rec.Budget.budget(), rest: frames, wait: wait})
}

// record answers false when another replica already holds this path: the caller must not run it too.
func (d *durableRuns) record(ctx context.Context, c continuation) bool {
	if d == nil {
		return true
	}
	rec, ok := newDurableRecord(c)
	if !ok {
		d.p.log.Warn("automations: paused run has no plan position, staying in memory", zap.String("run_id", c.run.ID))
		return true
	}
	return d.save(ctx, c.run, &rec)
}

func (d *durableRuns) recordWaiter(ctx context.Context, run *AutomationRun, turn queueTurn) bool {
	if d == nil {
		return true
	}
	rec := newDurableWaiter(run, turn)
	return d.save(ctx, run, &rec)
}

func (d *durableRuns) save(ctx context.Context, run *AutomationRun, rec *durableRecord) bool {
	body, err := codec.Marshal(rec)
	if err != nil {
		d.p.log.Warn("automations: paused run not recorded", zap.String("run_id", run.ID), zap.Error(err))
		return true
	}
	save := DurableSave{Body: body, Fresh: run.Durable == nil}
	if rec.Seat != nil {
		save.Seat = *rec.Seat
	}
	save.Ref = durableRefFor(run, rec.AutomationID)
	callCtx, cancel := d.callCtx(ctx)
	defer cancel()
	standing, err := d.store.Save(callCtx, d.owner, save)
	if err != nil {
		d.p.log.Warn("automations: valkey unavailable, paused run stays in memory", zap.String("run_id", run.ID), zap.Error(err))
		return true
	}
	if standing == DurableLost {
		return false
	}
	run.Durable = &durableTicket{ref: save.Ref, ticket: save.Seat.Ticket}
	if d.ctx.Err() != nil {
		d.release(ctx)
	}
	return true
}

func durableRefFor(run *AutomationRun, automationID string) DurableRef {
	if run.Durable != nil {
		return run.Durable.ref
	}
	return DurableRef{BroadcasterID: run.BroadcasterID, ID: nestedReplayKey(run.replayKey(), automationID)}
}

// A store error keeps running: only a record another replica holds stops this one.
func (d *durableRuns) stillOurs(ctx context.Context, run *AutomationRun) bool {
	if d == nil || run.Durable == nil {
		return true
	}
	callCtx, cancel := d.callCtx(ctx)
	defer cancel()
	standing, err := d.store.Standing(callCtx, d.owner, run.Durable.ref)
	return err != nil || standing != DurableLost
}

// finish answers false when another replica took the run: the outcome is theirs to record.
func (d *durableRuns) finish(ctx context.Context, run *AutomationRun) bool {
	if d == nil || run.Durable == nil {
		return true
	}
	callCtx, cancel := d.callCtx(ctx)
	defer cancel()
	standing, err := d.store.Finish(callCtx, d.owner, run.Durable.ref, run.Durable.ticket)
	if err != nil {
		d.p.log.Warn("automations: paused run record not removed, retrying", zap.String("run_id", run.ID), zap.Error(err))
		d.retryLater(run)
		return true
	}
	return standing != DurableLost
}

func (d *durableRuns) retryLater(run *AutomationRun) {
	left := unfinishedRun{ticket: *run.Durable, seat: RunQueueSeat{BroadcasterID: run.BroadcasterID, Ticket: run.Durable.ticket}}
	if run.Slot != nil {
		left.seat = run.Slot.seat
	}
	d.mu.Lock()
	d.unfinished = append(d.unfinished, left)
	d.mu.Unlock()
}

func (d *durableRuns) retryUnfinished(ctx context.Context) {
	d.mu.Lock()
	pending := d.unfinished
	d.unfinished = nil
	d.mu.Unlock()
	for _, left := range pending {
		d.retryFinish(ctx, left)
	}
}

func (d *durableRuns) retryFinish(ctx context.Context, left unfinishedRun) {
	callCtx, cancel := d.callCtx(ctx)
	defer cancel()
	if _, err := d.store.Finish(callCtx, d.owner, left.ticket.ref, left.ticket.ticket); err != nil {
		d.mu.Lock()
		d.unfinished = append(d.unfinished, left)
		d.mu.Unlock()
		return
	}
	if left.seat.Queue != "" {
		d.p.leaveQueue(left.seat)
	}
}
