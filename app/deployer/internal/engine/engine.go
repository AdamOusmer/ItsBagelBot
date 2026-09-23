// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package engine owns run lifecycle: validation, the stage runner, the lock
// heartbeat, cancel and approve, resume after restart, and the plan.
//
// At most one run executes in this process at a time, and the cluster lock in
// the store keeps it to one across processes. The run's KV revision fences
// the rest: every write is a compare-and-set, so when a second process takes
// a run over (the deployer rolling itself resumes the run from the new pod
// while the old pod is still draining) the old process loses its next write
// and stops driving instead of racing the new one.
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

// progressEvery caps cosmetic writes (progress bars, item rows, links) at two
// a second. A rollout reports on every pod event and a build on every job
// poll; each write is a KV put on the hub plus an event every open Deploys
// page renders, and nothing on the page moves faster than that. State
// transitions are never coalesced.
const progressEvery = 500 * time.Millisecond

// Deps are the engine's collaborators.
type Deps struct {
	Store  ports.Store
	Events ports.Events
	// Stages are every stage implementation; the engine orders them per run
	// with deploy.StagesFor.
	Stages []stage.Stage
	// Stage are the ports handed to stages, also used by Plan.
	Stage stage.Deps
	// Services are the rollout units in rollout order (cluster.Order()).
	Services []string
	Log      *zap.Logger
}

// Engine runs deploys.
type Engine struct {
	d      Deps
	stages map[deploy.StageID]stage.Stage
	// progressEvery is the cosmetic write interval; tests shorten or stretch it.
	progressEvery time.Duration
	// endRetry is the first backoff of persistEnd; tests shorten it.
	endRetry time.Duration

	// base is the process context runs execute under, set by Run before
	// ready closes; nothing reads it until ready is closed.
	base  context.Context
	ready chan struct{}

	// startMu serialises the verbs that check the store and then claim the
	// cluster (start, resume, cancel of an idle run), so two of them cannot
	// both pass the check.
	startMu sync.Mutex

	mu  sync.Mutex
	cur *execution
	// draining is set once Run stops waiting for work; no execution starts
	// after it, so the WaitGroup never grows under Run's Wait.
	draining bool
	wg       sync.WaitGroup
}

// New builds an engine. Call Run to start the background loop.
func New(d Deps) *Engine {
	stages := make(map[deploy.StageID]stage.Stage, len(d.Stages))
	for _, s := range d.Stages {
		stages[s.ID()] = s
	}
	return &Engine{d: d, stages: stages, progressEvery: progressEvery, endRetry: time.Second, ready: make(chan struct{})}
}

// Run resumes the active run from the store, then executes started runs
// until ctx ends. It returns nil on shutdown once the executing run has
// stopped; the run stays active in the store and the next process resumes it.
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

// Start validates, persists the new run and returns it at once; the stages
// run in the background.
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

// Get returns the run, from memory while it executes here (the freshest
// progress may still be coalescing) and from the store otherwise.
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

// List returns recent runs, newest first, and the lock holder.
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

// Resume restarts a failed (or verify-failed) run from its failed stage.
// Stages that finished stay finished; the failed one runs again from Done.
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

// Cancel asks the executing run to stop at its next safe point, or abandons
// a failed run so it can no longer be resumed.
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

// Approve answers the approval the executing run's stage is waiting on.
// An empty req.Stage answers whichever stage is waiting.
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

// executing returns the run executing in this process when it is id.
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

// claimCluster refuses when another run is active. The lock alone would
// also refuse, but only while its holder heartbeats: a process that died
// mid-run leaves an active run whose lock expires two minutes later, and
// starting a second run beside it would orphan the first.
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

// launch persists run (rev 0 creates it) and executes it in the background
// under the process context, never the caller's.
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
	// A verb that lands while the process drains leaves the run stored as
	// active with its lock held; the next pod resumes it from there.
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

// resumeActive picks up the run a previous process left active. The lock is
// re-acquired under the same owner, which succeeds even before the old
// holder's heartbeat expires; a different owner means two runs claim the
// cluster, and the stored one is failed rather than executed beside it.
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

// resumable loads a run the resume verb may restart.
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

// rerunBuild reruns the failed jobs of the build a failed build stage was
// waiting on, so the resumed stage waits on the rerun instead of seeing the
// same failure again.
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

// abandon cancels a stopped run that is not executing, which takes it out
// of the resumable set.
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

// writeStored writes a run no execution owns, with the same seq and publish
// discipline an execution uses.
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

// idle refuses a run the resume and cancel-abandon verbs may not take: one
// neither stopped nor orphaned.
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

// orphaned reports an active run nobody drives: not executing here, and its
// lock expired (or another run's). A driver heartbeats every
// Config.HeartbeatEvery, so an expired lock proves no process holds it.
// This is where a run lands when its ending write never reached the store
// before the execution stopped (see persistEnd); without it the run stayed
// active and refused start, resume and cancel until a pod restart.
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

// stopped reports the states resume and abandon act on: failed runs, and
// verify-failed ones (everything rolled out; a resume re-runs verify, which
// helps when a probe failed on something transient).
func stopped(s deploy.RunState) bool {
	return s == deploy.RunFailed || s == deploy.RunVerifyFailed
}

// reopen returns a stopped run to running with its failed stage pending.
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
