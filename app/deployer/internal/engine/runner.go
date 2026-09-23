// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// endWriteTimeout bounds the writes an execution makes after its own
// context stopped (failing a run whose lock was lost, releasing the lock),
// so a hub outage cannot hold the process past its grace period.
const endWriteTimeout = 5 * time.Second

// endRetryMax caps the backoff between retries of a write that settles a
// stage or ends the run. The retries last as long as the execution does:
// only shutdown, a lost lock or a proven takeover stops them.
const endRetryMax = 30 * time.Second

// outcome is how one stage attempt ended. The zero outcome means the
// execution stopped driving (shutdown, takeover, lost lock) and records
// nothing on the stage; run is the run state the outcome ends the run in,
// empty while the train goes on.
type outcome struct {
	state deploy.StageState
	fail  *ports.Fail
	run   deploy.RunState
}

var (
	outSucceeded = outcome{state: deploy.StateSucceeded}
	outSkipped   = outcome{state: deploy.StateSkipped}
	outCancelled = outcome{state: deploy.StateCancelled, run: deploy.RunCancelled}
)

// failed ends the run as failed, or as verify_failed when verify is the
// stage: by then every service rolled, so the page says "deployed, verify
// failed" and offers rollback instead of calling the deploy failed.
func failed(id deploy.StageID, f *ports.Fail) outcome {
	end := deploy.RunFailed
	if id == deploy.StageVerify {
		end = deploy.RunVerifyFailed
	}
	return outcome{state: deploy.StateFailed, fail: f, run: end}
}

// apply writes the outcome onto the stage and, when it ends the run, onto
// the run. The failure is shared by both so the page's banner and the
// stage row read the same code, message and log tail.
func (o outcome) apply(r *deploy.Run, id deploy.StageID, at time.Time) {
	s := r.Stage(id)
	s.State, s.EndedAt, s.Waiting, s.NeedsApproval = o.state, &at, "", ""
	if o.fail != nil {
		failure := o.fail.Failure
		if len(failure.Actions) == 0 {
			failure.Actions = defaultActions(id)
		}
		s.Failure, r.Failure = &failure, &failure
	}
	if o.run != "" {
		r.State = o.run
	}
}

// execute drives the run until it ends or this process stops driving it.
func (x *execution) execute() {
	defer x.e.wg.Done()
	go x.heartbeat()
	x.recordLive()
	x.drive()
	x.finish()
}

// drive runs the stages in order from the first unfinished one. Cancel is
// honoured here between stages, which is every stage's safe point.
func (x *execution) drive() {
	for {
		run := x.View()
		id := run.CurrentStage()
		switch {
		case x.ctx.Err() != nil:
			x.interrupted(id)
			return
		case id == "":
			x.conclude(deploy.RunSucceeded)
			return
		case run.CancelRequested && !inFlight(&run, id):
			x.conclude(deploy.RunCancelled)
			return
		}
		if !x.settle(id, x.runStage(id)) {
			return
		}
	}
}

// inFlight reports a cooperative stage a previous process left running: the
// cancel landed mid-stage and the process restarted before the stage reached
// its safe point. It runs once more so it can finish the unit it had in
// flight (rollout waits on the service it applied) and then honours the
// cancel itself; concluding here would leave that service rolling unwatched.
func inFlight(run *deploy.Run, id deploy.StageID) bool {
	s := run.Stage(id)
	return cooperative[id] && s.State == deploy.StateRunning
}

// runStage runs one attempt: Done first, and Run only when the effect does
// not exist yet. A Done error is recorded like a Run error, because a stage
// that cannot tell whether its effect exists must not guess.
func (x *execution) runStage(id deploy.StageID) outcome {
	ctx, release := x.stageContext(id)
	defer release()
	if err := x.begin(ctx, id); err != nil {
		return x.classify(id, err)
	}
	s, rc := x.e.stages[id], stage.New(id, x.e.d.Stage, x)
	done, err := s.Done(ctx, rc)
	if err == nil && !done {
		err = s.Run(ctx, rc)
	}
	return x.classify(id, err)
}

// begin marks the stage running. StartedAt restarts with every attempt, so
// the page times the attempt in flight rather than the first one.
func (x *execution) begin(ctx context.Context, id deploy.StageID) error {
	now := x.e.now()
	return x.Update(ctx, func(r *deploy.Run) {
		s := r.Stage(id)
		s.State, s.StartedAt, s.EndedAt, s.Attempts = deploy.StateRunning, &now, nil, s.Attempts+1
		s.Waiting, s.NeedsApproval, s.Failure = "", "", nil
		r.State = deploy.RunRunning
	})
}

// classify maps a stage's return onto the stage contract. A stopped
// execution context comes before the stage's own error: whatever the stage
// made of the cut (a *Fail wrapping context.Canceled, say), the run is not
// failed, and the next driver resumes it.
func (x *execution) classify(id deploy.StageID, err error) outcome {
	switch {
	case err == nil:
		return outSucceeded
	case errors.Is(err, stage.ErrSkipped):
		return outSkipped
	case x.ctx.Err() != nil:
		return outcome{}
	case errors.Is(err, stage.ErrCancelled), x.cutShort(id):
		return outCancelled
	}
	if f, ok := ports.AsFail(err); ok {
		return failed(id, f)
	}
	return failed(id, ports.Failf(deploy.FailInternal, "%v", err))
}

// settle records the attempt and reports whether the train goes on. An
// interrupted attempt records nothing: the stage stays running in the store
// and the next driver runs it again from Done.
func (x *execution) settle(id deploy.StageID, out outcome) bool {
	if out.state == "" {
		return true
	}
	now := x.e.now()
	x.persistEnd(func(r *deploy.Run) { out.apply(r, id, now) })
	if out.fail != nil {
		x.e.d.Log.Warn("deploy stage failed", zap.String("run", string(x.id)), zap.String("stage", string(id)),
			zap.String("code", string(out.fail.Code)), zap.String("message", out.fail.Message))
	}
	return out.run == ""
}

func (x *execution) conclude(end deploy.RunState) {
	x.persistEnd(func(r *deploy.Run) { r.State = end })
	x.e.d.Log.Info("deploy run ended", zap.String("run", string(x.id)), zap.String("state", string(end)))
}

// persistEnd applies a settling or ending edit and retries its write with
// backoff until it lands. A single logged failure used to strand the run:
// memory said ended, so finish released the lock and detached, while the
// store still named the run active, which refused start, resume and cancel
// alike until the pod restarted. The retries stop with the execution
// (shutdown, lost lock, takeover); the store then still holds the run as
// active and the next driver reconciles it through Done.
func (x *execution) persistEnd(edit func(*deploy.Run)) {
	err := x.Update(x.ctx, edit)
	for wait := x.e.endRetry; x.retryable(err); wait = min(2*wait, endRetryMax) {
		x.logPersist(err)
		if sleepCtx(x.ctx, wait) != nil {
			return
		}
		err = x.persist(x.ctx)
	}
	x.logPersist(err)
}

// retryable is a failed write this execution may still land: not a fence,
// not a closed execution, and not one whose context stopped.
func (x *execution) retryable(err error) bool {
	switch {
	case err == nil, x.ctx.Err() != nil:
		return false
	case errors.Is(err, errNotDriving), errors.Is(err, ports.ErrConflict):
		return false
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// interrupted handles a stop that is not the run's own ending. Shutdown and
// takeover leave the run as it is for whoever drives it next. A lost lock
// fails the run instead: the cluster is no longer provably ours, so
// carrying on needs the operator's resume, which takes the lock again.
func (x *execution) interrupted(id deploy.StageID) {
	x.mu.Lock()
	lost := x.lockLost
	x.mu.Unlock()
	if id == "" || !lost {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(x.ctx), endWriteTimeout)
	defer cancel()
	out := failed(id, ports.Failf(deploy.FailLockHeld, "cluster lock lost: no heartbeat landed within the lock TTL"))
	now := x.e.now()
	x.logPersist(x.Update(ctx, func(r *deploy.Run) { out.apply(r, id, now) }))
}

// finish stops the heartbeat, refuses further writes and frees the lock
// when the store holds the run as ended. A run left active keeps its lock
// for the next driver: the resume on the next pod re-acquires it under the
// same owner, and once the lock expires the resume and cancel verbs take an
// undriven active run too.
func (x *execution) finish() {
	x.stop()
	<-x.hbDone
	x.mu.Lock()
	x.closed = true
	x.stopFlushLocked()
	ended, lock := x.saved.Terminal(), x.lock
	x.mu.Unlock()
	if ended {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(x.ctx), endWriteTimeout)
		x.e.releaseLock(ctx, lock)
		cancel()
	}
	x.e.detach(x)
}

// recordLive notes the release the cluster runs before the train changes
// anything; the acl stage diffs deploy/messaging from its commit. It runs
// once, before the first stage starts, and is best effort: acl treats an
// unknown live commit as changed, which costs one no-op apply.
func (x *execution) recordLive() {
	run := x.View()
	if run.Outputs.LiveSHA != "" || run.Stages[0].StartedAt != nil {
		return
	}
	v, sha, err := x.e.liveRelease(x.ctx)
	if err != nil {
		x.e.d.Log.Warn("deploy live release unknown", zap.String("run", string(x.id)), zap.Error(err))
		return
	}
	x.logPersist(x.Update(x.ctx, func(r *deploy.Run) { r.Outputs.LiveVersion, r.Outputs.LiveSHA = v, sha }))
}

// logPersist logs a failed write of the engine's own. A lost CAS already
// fenced the execution, and a store outage leaves the run as last written,
// which a resume reconciles through Done.
func (x *execution) logPersist(err error) {
	if err != nil {
		x.e.d.Log.Warn("deploy run persist failed", zap.String("run", string(x.id)), zap.Error(err))
	}
}
