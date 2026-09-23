// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// errNotDriving refuses a write from a run this process no longer drives:
// it finished, or another process took it over (lost KV compare-and-set).
var errNotDriving = errors.New("run is no longer driven by this process")

// cooperative stages are the ones the cancel verb must not interrupt
// mid-flight. Rollout finishes the service in flight and then stops, because
// a Deployment abandoned half rolled holds old and new pods side by side
// with nobody watching it converge. Every other stage is idempotent at any
// point, so cancel cuts its context and the stage stops at once instead of
// sitting out a build or a checks wait.
var cooperative = map[deploy.StageID]bool{deploy.StageRollout: true}

// execution is one run executing in this process: the stage.Sink its stages
// write through, the lock heartbeat, and what the cancel and approve verbs
// signal.
type execution struct {
	e  *Engine
	id deploy.RunID
	// ctx is cancelled when this process must stop driving the run: the
	// lock was lost, the run was taken over, or the run ended.
	ctx    context.Context
	stop   context.CancelFunc
	hbDone chan struct{}
	// cancelled ends when the cancel verb lands. It ends approval waits and
	// cuts every stage that is not cooperative; a stage context registered
	// after the cancel is cut at once, so no verb can slip between the check
	// at the top of the train and the stage starting.
	cancelled    context.Context
	signalCancel context.CancelFunc

	mu  sync.Mutex
	run deploy.Run
	rev ports.Revision
	// saved is the run state the store last acknowledged. finish reads it,
	// not run.State: an ending the store never took leaves the run active
	// for the next driver, and releasing the lock over it would strand it.
	saved deploy.RunState
	// attempts are the writes sent since the last acknowledged one, seq to
	// UpdatedAt. A write can land while its ack is lost (a hub leader
	// change on the R3 bucket); the next write then loses the CAS to our
	// own record, and these tell that apart from a takeover.
	attempts map[uint64]time.Time
	lock     ports.Lock
	lockLost bool
	fenced   bool
	closed   bool
	approval *approval

	lastPut   time.Time
	flush     *time.Timer
	flushGen  uint64
	throttled bool
}

type approval struct {
	stage   deploy.StageID
	granted chan struct{}
}

var _ stage.Sink = (*execution)(nil)

func newExecution(e *Engine, run deploy.Run, rev ports.Revision, lock ports.Lock) *execution {
	ctx, stop := context.WithCancel(e.base)
	cancelled, signal := context.WithCancel(context.Background())
	x := &execution{
		e: e, id: run.ID, ctx: ctx, stop: stop, hbDone: make(chan struct{}),
		cancelled: cancelled, signalCancel: signal, run: run, rev: rev, lock: lock,
		saved: run.State, attempts: map[uint64]time.Time{},
	}
	if run.CancelRequested {
		signal()
	}
	return x
}

// View returns a copy of the run safe to marshal while stages keep writing.
func (x *execution) View() deploy.Run {
	x.mu.Lock()
	defer x.mu.Unlock()
	return cloneRun(&x.run)
}

// Update applies edit and persists it. An edit that changes nothing but
// cosmetic progress is coalesced to progressEvery; the pending write lands
// on a timer, or with the next state transition, whichever comes first.
func (x *execution) Update(ctx context.Context, edit func(*deploy.Run)) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed || x.fenced {
		return errNotDriving
	}
	before := stateOf(&x.run)
	edit(&x.run)
	if bytes.Equal(before, stateOf(&x.run)) && x.coalesce() {
		return nil
	}
	return x.persistLocked(ctx)
}

// AwaitApproval marks the stage waiting on reason and blocks until the
// approve verb, the cancel verb, or ctx.
func (x *execution) AwaitApproval(ctx context.Context, id deploy.StageID, reason string) error {
	a, err := x.openApproval(ctx, id, reason)
	if err != nil {
		return err
	}
	defer x.dropApproval(a)
	select {
	case <-a.granted:
		return nil
	case <-x.cancelled.Done():
		return stage.ErrCancelled
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (x *execution) openApproval(ctx context.Context, id deploy.StageID, reason string) (*approval, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if s := x.run.Stage(id); s != nil {
		s.State, s.NeedsApproval = deploy.StateWaiting, reason
	}
	x.run.State = deploy.RunWaiting
	if err := x.persistLocked(ctx); err != nil {
		return nil, err
	}
	x.approval = &approval{stage: id, granted: make(chan struct{})}
	return x.approval, nil
}

// dropApproval forgets a wait that ended without the approve verb (cancel,
// shutdown), so a late approve is refused instead of answering nobody.
func (x *execution) dropApproval(a *approval) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.approval == a {
		x.approval = nil
	}
}

// approve answers the waiting approval. The run is back to running in the
// reply, before the stage goroutine wakes.
func (x *execution) approve(ctx context.Context, id deploy.StageID) (deploy.Run, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.awaiting(id) {
		return deploy.Run{}, fmt.Errorf("%w: no stage of run %s awaits approval", ports.ErrNotResumable, x.id)
	}
	a := x.approval
	x.approval = nil
	if s := x.run.Stage(a.stage); s != nil {
		s.State, s.NeedsApproval = deploy.StateRunning, ""
	}
	x.run.State = deploy.RunRunning
	err := x.persistLocked(ctx)
	close(a.granted)
	return cloneRun(&x.run), err
}

func (x *execution) awaiting(id deploy.StageID) bool {
	if x.approval == nil {
		return false
	}
	return id == "" || id == x.approval.stage
}

// requestCancel records the cancel and signals the running stage: an
// interruptible stage has its context cut, a cooperative one sees
// rc.Cancelled at its next safe point, and an approval wait ends. A run that
// already ended here answers errNotDriving, and the verb falls back to the
// stored run.
func (x *execution) requestCancel(ctx context.Context) (deploy.Run, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed || x.fenced {
		return deploy.Run{}, errNotDriving
	}
	var err error
	if !x.run.CancelRequested {
		x.run.CancelRequested = true
		err = x.persistLocked(ctx)
	}
	x.signalCancel()
	return cloneRun(&x.run), err
}

// stageContext is the context one stage runs under. A cooperative stage
// gets the execution's own context and stops at its safe points; every
// other stage's context is also cut by the cancel verb.
func (x *execution) stageContext(id deploy.StageID) (context.Context, context.CancelFunc) {
	if cooperative[id] {
		return x.ctx, func() {}
	}
	ctx, cancel := context.WithCancel(x.ctx)
	unhook := context.AfterFunc(x.cancelled, cancel)
	return ctx, func() {
		unhook()
		cancel()
	}
}

// cutShort reports whether the cancel verb cut the stage: its error is then
// the cut context, however the stage wrapped it.
func (x *execution) cutShort(id deploy.StageID) bool {
	return x.cancelled.Err() != nil && !cooperative[id]
}

func (x *execution) persist(ctx context.Context) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed || x.fenced {
		return errNotDriving
	}
	return x.persistLocked(ctx)
}

// persistLocked writes the run with the next seq and publishes the same
// snapshot. The KV write is the source of truth; a failed publish only
// delays the page until the next one, so it is logged, not returned.
func (x *execution) persistLocked(ctx context.Context) error {
	x.run.Seq++
	x.run.UpdatedAt = x.e.now()
	x.attempts[x.run.Seq] = x.run.UpdatedAt
	rev, err := x.e.d.Store.Put(ctx, &x.run, x.rev)
	if errors.Is(err, ports.ErrConflict) {
		rev, err = x.reconcileLocked(ctx)
	}
	if err != nil {
		return err
	}
	x.rev, x.lastPut, x.saved = rev, time.Now(), x.run.State
	clear(x.attempts)
	x.stopFlushLocked()
	if err := x.e.d.Events.Publish(ctx, &x.run); err != nil {
		x.e.d.Log.Warn("deploy event publish failed", zap.String("run", string(x.id)), zap.Error(err))
	}
	return nil
}

// reconcileLocked resolves a lost CAS. When the stored record is one of our
// own unacknowledged writes, the revision is adopted and the current run
// written over it (memory holds every edit that write carried). Anything
// else was written by another process, which now drives the run. A failed
// read proves neither, so it is returned without fencing and the next
// write tries again.
func (x *execution) reconcileLocked(ctx context.Context) (ports.Revision, error) {
	stored, rev, err := x.e.d.Store.Get(ctx, x.id)
	if err != nil {
		return 0, err
	}
	if !x.ownWrite(&stored) {
		x.fenceLocked()
		return 0, ports.ErrConflict
	}
	if stored.Seq == x.run.Seq {
		return rev, nil
	}
	next, err := x.e.d.Store.Put(ctx, &x.run, rev)
	if errors.Is(err, ports.ErrConflict) {
		x.fenceLocked()
	}
	return next, err
}

func (x *execution) ownWrite(stored *deploy.Run) bool {
	at, ok := x.attempts[stored.Seq]
	return ok && at.Equal(stored.UpdatedAt)
}

// fenceLocked stops driving a run whose revision moved under us: another
// process resumed it, and its writes win.
func (x *execution) fenceLocked() {
	x.fenced = true
	x.stopFlushLocked()
	x.stop()
	x.e.d.Log.Warn("deploy run taken over by another process", zap.String("run", string(x.id)))
}

// coalesce defers a cosmetic write that lands within progressEvery of the
// last one. It reports whether the write was deferred.
func (x *execution) coalesce() bool {
	wait := x.e.progressEvery - time.Since(x.lastPut)
	if wait <= 0 {
		return false
	}
	if x.flush == nil {
		gen := x.flushGen
		x.flush = time.AfterFunc(wait, func() { x.flushProgress(gen) })
	}
	return true
}

// flushProgress lands a deferred cosmetic write. gen goes stale when a full
// write landed in between, which already carried the progress.
func (x *execution) flushProgress(gen uint64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if gen != x.flushGen {
		return
	}
	x.flush = nil
	if err := x.persistLocked(x.ctx); err != nil {
		x.e.d.Log.Warn("deploy progress persist failed", zap.String("run", string(x.id)), zap.Error(err))
	}
}

func (x *execution) stopFlushLocked() {
	if x.flush != nil {
		x.flush.Stop()
		x.flush = nil
	}
	x.flushGen++
}

// heartbeat extends the cluster lock every Config.HeartbeatEvery until the
// execution stops. A lost CAS goes through reclaim, which stops the run only
// when another run holds the lock; any other error is retried on the next
// beat, since the lock survives LockTTL without one.
func (x *execution) heartbeat() {
	defer close(x.hbDone)
	t := time.NewTicker(x.e.d.Stage.Config.HeartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-x.ctx.Done():
			return
		case <-t.C:
			x.beat()
		}
	}
}

func (x *execution) beat() {
	x.mu.Lock()
	lock := x.lock
	x.mu.Unlock()
	next, err := x.e.d.Store.Heartbeat(x.ctx, lock)
	switch {
	case err == nil:
		x.mu.Lock()
		x.lock = next
		x.mu.Unlock()
	case errors.Is(err, ports.ErrLockHeld):
		x.reclaim()
	case x.ctx.Err() == nil:
		x.e.d.Log.Warn("deploy lock heartbeat failed", zap.String("run", string(x.id)), zap.Error(err))
	}
}

// reclaim settles a heartbeat that lost its CAS. The usual cause is our own
// previous beat landing with its ack lost (hub leader change on the R3
// bucket), which leaves the held revision stale while the lock is still
// ours. Re-acquiring under our own id adopts the current revision in that
// case and is refused only when another run holds an unexpired lock, which
// is the one outcome that means the cluster is no longer ours. Failing the
// run on the bare CAS loss cut a cooperative rollout mid-service and left a
// Deployment half rolled with nobody watching.
func (x *execution) reclaim() {
	next, err := x.e.d.Store.AcquireLock(x.ctx, x.id)
	switch {
	case err == nil:
		x.mu.Lock()
		x.lock = next
		x.mu.Unlock()
		x.e.d.Log.Info("deploy lock revision adopted after a lost heartbeat ack", zap.String("run", string(x.id)))
	case errors.Is(err, ports.ErrLockHeld):
		x.mu.Lock()
		x.lockLost = true
		x.mu.Unlock()
		x.stop()
		x.e.d.Log.Error("deploy lock lost", zap.String("run", string(x.id)), zap.Error(err))
	case x.ctx.Err() == nil:
		x.e.d.Log.Warn("deploy lock re-read failed", zap.String("run", string(x.id)), zap.Error(err))
	}
}
