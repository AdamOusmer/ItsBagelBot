// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
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

const progressEvery = 500 * time.Millisecond

type Deps struct {
	Store    ports.Store
	Events   ports.Events
	Stages   []stage.Stage
	Stage    stage.Deps
	Services []string
	Log      *zap.Logger
}

type Engine struct {
	d             Deps
	stages        map[deploy.StageID]stage.Stage
	progressEvery time.Duration
	endRetry      time.Duration

	// base is set by Run before ready closes; read it only after ready.
	base  context.Context
	ready chan struct{}

	startMu sync.Mutex

	mu  sync.Mutex
	cur *execution
	// No execution starts once draining is set, so wg never grows under Run's Wait.
	draining bool
	wg       sync.WaitGroup
}

func New(d Deps) *Engine {
	stages := make(map[deploy.StageID]stage.Stage, len(d.Stages))
	for _, s := range d.Stages {
		stages[s.ID()] = s
	}
	return &Engine{d: d, stages: stages, progressEvery: progressEvery, endRetry: time.Second, ready: make(chan struct{})}
}

func (e *Engine) Run(ctx context.Context) error {
	e.base = ctx
	err := e.resumeActive(ctx)
	close(e.ready)
	if err != nil {
		return err
	}
	<-ctx.Done()
	e.mu.Lock()
	e.draining = true
	e.mu.Unlock()
	e.wg.Wait()
	return nil
}

func (e *Engine) Start(ctx context.Context, actor deploy.Actor, req deploy.StartRequest) (deploy.Run, error) {
	if err := e.awaitReady(ctx); err != nil {
		return deploy.Run{}, err
	}
	e.startMu.Lock()
	defer e.startMu.Unlock()

	run, err := e.newRun(ctx, actor, req)
	if err != nil {
		return deploy.Run{}, err
	}
	if err := e.claimCluster(ctx, run.ID); err != nil {
		return deploy.Run{}, err
	}
	lock, err := e.d.Store.AcquireLock(ctx, run.ID)
	if err != nil {
		return deploy.Run{}, err
	}
	e.d.Log.Info("deploy run started", zap.String("run", string(run.ID)),
		zap.String("kind", string(run.Kind)), zap.String("actor", actor.Login))
	return e.launch(ctx, run, 0, lock)
}

func (e *Engine) Get(ctx context.Context, _ deploy.Actor, req deploy.RunRequest) (deploy.Run, error) {
	if req.RunID == "" {
		return deploy.Run{}, invalid("run_id is required")
	}
	if x := e.executing(req.RunID); x != nil {
		return x.View(), nil
	}
	run, _, err := e.d.Store.Get(ctx, req.RunID)
	return run, err
}

func (e *Engine) List(ctx context.Context, _ deploy.Actor, req deploy.ListRequest) ([]deploy.RunSummary, deploy.RunID, error) {
	limit := req.Limit
	if limit <= 0 || limit > deploy.KeepRuns {
		limit = deploy.KeepRuns
	}
	runs, err := e.d.Store.List(ctx, limit)
	if err != nil {
		return nil, "", err
	}
	owner, _, err := e.d.Store.LockOwner(ctx)
	if err != nil {
		return nil, "", err
	}
	if runs == nil {
		runs = []deploy.RunSummary{}
	}
	return runs, owner, nil
}

func (e *Engine) Resume(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error) {
	if err := e.awaitReady(ctx); err != nil {
		return deploy.Run{}, err
	}
	e.startMu.Lock()
	defer e.startMu.Unlock()

	run, rev, err := e.resumable(ctx, req.RunID)
	if err != nil {
		return deploy.Run{}, err
	}
	if err := e.claimCluster(ctx, run.ID); err != nil {
		return deploy.Run{}, err
	}
	if req.Rerun {
		if err := e.rerunBuild(ctx, &run); err != nil {
			return deploy.Run{}, err
		}
	}
	lock, err := e.d.Store.AcquireLock(ctx, run.ID)
	if err != nil {
		return deploy.Run{}, err
	}
	reopen(&run)
	e.d.Log.Info("deploy run resumed", zap.String("run", string(run.ID)),
		zap.String("stage", string(run.CurrentStage())), zap.String("actor", actor.Login))
	return e.launch(ctx, run, rev, lock)
}

func (e *Engine) Cancel(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error) {
	e.startMu.Lock()
	defer e.startMu.Unlock()

	e.d.Log.Info("deploy run cancel requested", zap.String("run", string(req.RunID)), zap.String("actor", actor.Login))
	x := e.executing(req.RunID)
	if x == nil {
		return e.abandon(ctx, req.RunID)
	}
	run, err := x.requestCancel(ctx)
	if errors.Is(err, errNotDriving) {
		return e.abandon(ctx, req.RunID)
	}
	return run, err
}

func (e *Engine) Approve(ctx context.Context, actor deploy.Actor, req deploy.RunRequest) (deploy.Run, error) {
	x := e.executing(req.RunID)
	if x == nil {
		return deploy.Run{}, e.notExecuting(ctx, req.RunID)
	}
	e.d.Log.Info("deploy stage approved", zap.String("run", string(req.RunID)),
		zap.String("stage", string(req.Stage)), zap.String("actor", actor.Login))
	return x.approve(ctx, req.Stage)
}

func (e *Engine) awaitReady(ctx context.Context) error {
	select {
	case <-e.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) now() time.Time { return e.d.Stage.Clock.Now() }

func (e *Engine) executing(id deploy.RunID) *execution {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cur != nil && e.cur.id == id {
		return e.cur
	}
	return nil
}

func (e *Engine) detach(x *execution) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cur == x {
		e.cur = nil
	}
}

func (e *Engine) claimCluster(ctx context.Context, id deploy.RunID) error {
	active, _, ok, err := e.d.Store.Active(ctx)
	if err != nil {
		return err
	}
	if ok && active.ID != id {
		return fmt.Errorf("%w: run %s is active", ports.ErrConflict, active.ID)
	}
	return nil
}

func (e *Engine) launch(ctx context.Context, run deploy.Run, rev ports.Revision, lock ports.Lock) (deploy.Run, error) {
	if err := e.knowsStages(run.Stages); err != nil {
		e.releaseLock(ctx, lock)
		return deploy.Run{}, err
	}
	x := newExecution(e, run, rev, lock)
	if err := x.persist(ctx); err != nil {
		e.releaseLock(ctx, lock)
		return deploy.Run{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.draining {
		e.cur = x
		e.wg.Add(1)
		go x.execute()
	}
	return x.View(), nil
}

func (e *Engine) knowsStages(stages []deploy.Stage) error {
	for _, s := range stages {
		if e.stages[s.ID] == nil {
			return fmt.Errorf("stage %s has no implementation", s.ID)
		}
	}
	return nil
}

func (e *Engine) releaseLock(ctx context.Context, lock ports.Lock) {
	if err := e.d.Store.ReleaseLock(ctx, lock); err != nil {
		e.d.Log.Warn("deploy lock release failed", zap.String("run", string(lock.Owner)), zap.Error(err))
	}
}

func (e *Engine) resumeActive(ctx context.Context) error {
	run, rev, ok, err := e.d.Store.Active(ctx)
	if err != nil || !ok {
		return err
	}
	lock, err := e.d.Store.AcquireLock(ctx, run.ID)
	if errors.Is(err, ports.ErrLockHeld) {
		return e.failStored(ctx, run, rev, ports.Failf(deploy.FailLockHeld, "cluster lock held by another run at restart"))
	}
	if err != nil {
		return err
	}
	e.d.Log.Info("deploy run resumed after restart", zap.String("run", string(run.ID)),
		zap.String("stage", string(run.CurrentStage())))
	_, err = e.launch(ctx, run, rev, lock)
	return err
}

func (e *Engine) resumable(ctx context.Context, id deploy.RunID) (deploy.Run, ports.Revision, error) {
	if e.executing(id) != nil {
		return deploy.Run{}, 0, fmt.Errorf("%w: run %s is executing", ports.ErrNotResumable, id)
	}
	run, rev, err := e.d.Store.Get(ctx, id)
	if err != nil {
		return deploy.Run{}, 0, err
	}
	return run, rev, e.idle(ctx, &run)
}

func (e *Engine) rerunBuild(ctx context.Context, run *deploy.Run) error {
	if run.CurrentStage() != deploy.StageBuild || run.Outputs.BuildRunID == 0 {
		return invalid("rerun applies to a failed build stage")
	}
	wr, err := e.d.Stage.GitHub.WorkflowRun(ctx, run.Outputs.BuildRunID)
	if err != nil {
		return err
	}
	if err := e.d.Stage.GitHub.RerunFailedJobs(ctx, wr.ID); err != nil {
		return err
	}
	run.Outputs.BuildRunAttempt = wr.Attempt + 1
	return nil
}

func (e *Engine) abandon(ctx context.Context, id deploy.RunID) (deploy.Run, error) {
	run, rev, err := e.d.Store.Get(ctx, id)
	if err != nil {
		return deploy.Run{}, err
	}
	if err := e.idle(ctx, &run); err != nil {
		return deploy.Run{}, err
	}
	run.State, run.CancelRequested = deploy.RunCancelled, true
	return run, e.writeStored(ctx, &run, rev)
}

func (e *Engine) failStored(ctx context.Context, run deploy.Run, rev ports.Revision, f *ports.Fail) error {
	now := e.now()
	failure := f.Failure
	failure.Actions = defaultActions(run.CurrentStage())
	if s := run.Stage(run.CurrentStage()); s != nil {
		s.State, s.EndedAt, s.Failure = deploy.StateFailed, &now, &failure
	}
	run.State, run.Failure = deploy.RunFailed, &failure
	return e.writeStored(ctx, &run, rev)
}

func (e *Engine) writeStored(ctx context.Context, run *deploy.Run, rev ports.Revision) error {
	run.Seq++
	run.UpdatedAt = e.now()
	if _, err := e.d.Store.Put(ctx, run, rev); err != nil {
		return err
	}
	if err := e.d.Events.Publish(ctx, run); err != nil {
		e.d.Log.Warn("deploy event publish failed", zap.String("run", string(run.ID)), zap.Error(err))
	}
	return nil
}

func (e *Engine) notExecuting(ctx context.Context, id deploy.RunID) error {
	run, _, err := e.d.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: run %s is %s and awaits no approval", ports.ErrNotResumable, id, run.State)
}

func (e *Engine) idle(ctx context.Context, run *deploy.Run) error {
	if stopped(run.State) {
		return nil
	}
	orphan, err := e.orphaned(ctx, run)
	if err != nil || orphan {
		return err
	}
	return fmt.Errorf("%w: run %s is %s", ports.ErrNotResumable, run.ID, run.State)
}

func (e *Engine) orphaned(ctx context.Context, run *deploy.Run) (bool, error) {
	if run.State.Terminal() || e.executing(run.ID) != nil {
		return false, nil
	}
	owner, held, err := e.d.Store.LockOwner(ctx)
	if err != nil {
		return false, err
	}
	return !held || owner != run.ID, nil
}

func stopped(s deploy.RunState) bool {
	return s == deploy.RunFailed || s == deploy.RunVerifyFailed
}

func reopen(run *deploy.Run) {
	run.State, run.Failure, run.CancelRequested = deploy.RunRunning, nil, false
	for i := range run.Stages {
		s := &run.Stages[i]
		if s.State == deploy.StateFailed || s.State == deploy.StateCancelled {
			s.State, s.Failure, s.EndedAt = deploy.StatePending, nil, nil
		}
	}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ports.ErrInvalid, fmt.Sprintf(format, args...))
}
