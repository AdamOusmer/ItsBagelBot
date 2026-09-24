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

const endWriteTimeout = 5 * time.Second

const endRetryMax = 30 * time.Second

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

func failed(id deploy.StageID, f *ports.Fail) outcome {
	end := deploy.RunFailed
	if id == deploy.StageVerify {
		end = deploy.RunVerifyFailed
	}
	return outcome{state: deploy.StateFailed, fail: f, run: end}
}

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

func (x *execution) execute() {
	defer x.e.wg.Done()
	go x.heartbeat()
	x.recordLive()
	x.drive()
	x.finish()
}

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

func inFlight(run *deploy.Run, id deploy.StageID) bool {
	s := run.Stage(id)
	return cooperative[id] && s.State == deploy.StateRunning
}

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

func (x *execution) begin(ctx context.Context, id deploy.StageID) error {
	now := x.e.now()
	return x.Update(ctx, func(r *deploy.Run) {
		s := r.Stage(id)
		s.State, s.StartedAt, s.EndedAt, s.Attempts = deploy.StateRunning, &now, nil, s.Attempts+1
		s.Waiting, s.NeedsApproval, s.Failure = "", "", nil
		r.State = deploy.RunRunning
	})
}

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

func (x *execution) logPersist(err error) {
	if err != nil {
		x.e.d.Log.Warn("deploy run persist failed", zap.String("run", string(x.id)), zap.Error(err))
	}
}
