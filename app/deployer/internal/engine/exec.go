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

var errNotDriving = errors.New("run is no longer driven by this process")

var cooperative = map[deploy.StageID]bool{deploy.StageRollout: true}

type execution struct {
	e            *Engine
	id           deploy.RunID
	ctx          context.Context
	stop         context.CancelFunc
	hbDone       chan struct{}
	cancelled    context.Context
	signalCancel context.CancelFunc

	mu  sync.Mutex
	run deploy.Run
	rev ports.Revision
	// finish must read saved, not run.State: releasing the lock over an unsaved ending strands the run.
	saved    deploy.RunState
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

func (x *execution) View() deploy.Run {
	x.mu.Lock()
	defer x.mu.Unlock()
	return cloneRun(&x.run)
}

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

func (x *execution) dropApproval(a *approval) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.approval == a {
		x.approval = nil
	}
}

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

func (x *execution) fenceLocked() {
	x.fenced = true
	x.stopFlushLocked()
	x.stop()
	x.e.d.Log.Warn("deploy run taken over by another process", zap.String("run", string(x.id)))
}

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

// Only another run's unexpired lock stops the run: failing on a bare CAS loss cuts a rollout.
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
