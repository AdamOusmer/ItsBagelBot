// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

var bg = context.Background()

func TestTrainRunsTheKindsStagesInOrder(t *testing.T) {
	cases := []struct {
		name     string
		req      deploy.StartRequest
		tags     map[deploy.Version]deploy.SHA
		releases []ports.Release
	}{
		{name: "release", req: deploy.StartRequest{Kind: deploy.KindRelease, Version: "v1.2.3-beta", PRs: []int{7}}},
		{
			name: "hotfix", req: deploy.StartRequest{Kind: deploy.KindHotfix, Version: "v1.2.2-beta"},
			tags: map[deploy.Version]deploy.SHA{"v1.2.2-beta": "abc"},
		},
		{name: "bump", req: deploy.StartRequest{Kind: deploy.KindBump, Services: []string{"gossip"}}},
		{
			name: "rollback", req: deploy.StartRequest{Kind: deploy.KindRollback, RollbackTo: "v1.2.1-beta"},
			releases: []ports.Release{{Tag: "v1.2.1-beta"}},
		},
		{name: "reapply", req: deploy.StartRequest{Kind: deploy.KindReapply}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.gh.tags, h.gh.releases = tc.tags, tc.releases
			h.boot()

			run := h.await(h.start(tc.req).ID, deploy.RunSucceeded)

			ids := deploy.StagesFor(tc.req.Kind)
			assert.Equal(t, train{Ran: ids, States: statesFrom(ids, deploy.StateSucceeded), SeqsIncrease: true},
				train{Ran: h.script.calls(), States: stageStates(run), SeqsIncrease: increasing(h.events.published())})
		})
	}
}

type train struct {
	Ran          []deploy.StageID
	States       map[deploy.StageID]deploy.StageState
	SeqsIncrease bool
}

func increasing(seqs []uint64) bool {
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			return false
		}
	}
	return len(seqs) > 0
}

func TestDoneStageSucceedsWithoutRunning(t *testing.T) {
	h := newHarness(t)
	h.script.markDone(deploy.StageACL)
	h.boot()

	run := h.await(h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID, deploy.RunSucceeded)

	ids := deploy.StagesFor(deploy.KindReapply)
	assert.Equal(t,
		train{Ran: slices.DeleteFunc(slices.Clone(ids), isACL), States: statesFrom(ids, deploy.StateSucceeded)},
		train{Ran: h.script.calls(), States: stageStates(run)})
}

func isACL(id deploy.StageID) bool { return id == deploy.StageACL }

// failOnce fails the first attempt with err and succeeds on the next.
func failOnce(err error) body {
	tripped := false
	return func(context.Context, *stage.RunCtx) error {
		if tripped {
			return nil
		}
		tripped = true
		return err
	}
}

type failureView struct {
	Run     deploy.RunState
	Code    deploy.FailureCode
	Actions []deploy.Action
	Ran     []deploy.StageID
	Later   map[deploy.StageID]deploy.StageState
}

func TestFailureStopsTheTrainAndResumeContinuesFromIt(t *testing.T) {
	cases := []struct {
		name    string
		stage   deploy.StageID
		err     error
		run     deploy.RunState
		code    deploy.FailureCode
		actions []deploy.Action
	}{
		{
			name: "build failure offers a rerun", stage: deploy.StageBuild,
			err: ports.Failf(deploy.FailBuildFailed, "job failed"), run: deploy.RunFailed, code: deploy.FailBuildFailed,
			actions: []deploy.Action{deploy.ActionResume, deploy.ActionRerun, deploy.ActionCancel},
		},
		{
			name: "bare error is internal", stage: deploy.StageTag,
			err: errors.New("boom"), run: deploy.RunFailed, code: deploy.FailInternal,
			actions: []deploy.Action{deploy.ActionResume, deploy.ActionCancel},
		},
		{
			name: "verify failure means deployed", stage: deploy.StageVerify,
			err: ports.Failf(deploy.FailVerify, "probe"), run: deploy.RunVerifyFailed, code: deploy.FailVerify,
			actions: []deploy.Action{deploy.ActionResume, deploy.ActionRollback, deploy.ActionCancel},
		},
		{
			name: "stage actions are kept", stage: deploy.StageRollout,
			err: &ports.Fail{Failure: deploy.Failure{Code: deploy.FailCrashLoop, Actions: []deploy.Action{deploy.ActionResume}}},
			run: deploy.RunFailed, code: deploy.FailCrashLoop, actions: []deploy.Action{deploy.ActionResume},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).boot()
			h.script.set(tc.stage, failOnce(tc.err))
			id := h.start(deploy.StartRequest{Kind: deploy.KindRelease, Version: "v1.2.3-beta"}).ID

			stopped := h.await(id, tc.run)

			ids := deploy.StagesFor(deploy.KindRelease)
			at := slices.Index(ids, tc.stage)
			assert.Equal(t,
				failureView{tc.run, tc.code, tc.actions, ids[:at+1], statesFrom(ids[at+1:], deploy.StatePending)},
				failureView{stopped.State, stopped.Failure.Code, stopped.Failure.Actions, h.script.calls(), later(stopped, at)})

			_, err := h.eng.Resume(bg, owner, deploy.RunRequest{RunID: id})
			require.NoError(t, err)
			resumed := h.await(id, deploy.RunSucceeded)
			assert.Equal(t, slices.Concat(ids[:at+1], ids[at:]), h.script.calls())
			assert.Equal(t, 2, resumed.Stage(tc.stage).Attempts)
		})
	}
}

func later(run deploy.Run, at int) map[deploy.StageID]deploy.StageState {
	return stageStates(deploy.Run{Stages: run.Stages[at+1:]})
}

func TestResumeRerunsTheFailedBuild(t *testing.T) {
	h := newHarness(t).boot()
	h.script.set(deploy.StageBuild, func(ctx context.Context, rc *stage.RunCtx) error {
		if err := rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.BuildRunID = 42 }); err != nil {
			return err
		}
		return ports.Failf(deploy.FailBuildFailed, "job failed")
	})
	id := h.start(deploy.StartRequest{Kind: deploy.KindBump}).ID
	h.await(id, deploy.RunFailed)
	h.script.set(deploy.StageBuild, nil)

	_, err := h.eng.Resume(bg, owner, deploy.RunRequest{RunID: id, Rerun: true})
	require.NoError(t, err)

	done := h.await(id, deploy.RunSucceeded)
	assert.Equal(t, [2]any{[]int64{42}, 2}, [2]any{h.gh.reruns, done.Outputs.BuildRunAttempt})
}

func TestRerunRefusedOutsideTheBuildStage(t *testing.T) {
	h := newHarness(t).boot()
	h.script.set(deploy.StageACL, failOnce(ports.Failf(deploy.FailTimeout, "reload")))
	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
	h.await(id, deploy.RunFailed)

	_, err := h.eng.Resume(bg, owner, deploy.RunRequest{RunID: id, Rerun: true})

	require.ErrorIs(t, err, ports.ErrInvalid)
}

// untilCancelled is a cooperative stage body: it stops at its safe point
// once cancel is requested. A cut context would make it fail instead, which
// the test would see as a failed run.
func untilCancelled(ctx context.Context, rc *stage.RunCtx) error {
	for !rc.Cancelled() {
		if err := ctx.Err(); err != nil {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	return stage.ErrCancelled
}

// untilCut waits on its context the way a build or checks wait does and
// wraps the cut the way the GitHub stages do.
func untilCut(ctx context.Context, _ *stage.RunCtx) error {
	<-ctx.Done()
	return ports.Failf(deploy.FailGitHub, "wait for build: %v", ctx.Err())
}

func awaitApproval(ctx context.Context, rc *stage.RunCtx) error {
	return rc.AwaitApproval(ctx, "apply messaging?")
}

func TestCancelStopsAtTheStagesSafePoint(t *testing.T) {
	cases := []struct {
		name  string
		stage deploy.StageID
		body  body
	}{
		{name: "cooperative rollout is not cut", stage: deploy.StageRollout, body: untilCancelled},
		{name: "build wait is cut", stage: deploy.StageBuild, body: untilCut},
		{name: "approval wait ends", stage: deploy.StageACL, body: awaitApproval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).boot()
			in := make(chan struct{})
			h.script.set(tc.stage, entered(in, tc.body))
			id := h.start(deploy.StartRequest{Kind: deploy.KindBump}).ID
			<-in

			_, err := h.eng.Cancel(bg, owner, deploy.RunRequest{RunID: id})
			require.NoError(t, err)

			run := h.await(id, deploy.RunCancelled)
			ids := deploy.StagesFor(deploy.KindBump)
			assert.Equal(t,
				cancelView{ids[:slices.Index(ids, tc.stage)+1], deploy.StateCancelled},
				cancelView{h.script.calls(), run.Stage(tc.stage).State})
		})
	}
}

type cancelView struct {
	Ran   []deploy.StageID
	Stage deploy.StageState
}

func TestCancelLetsTheCooperativeStageFinish(t *testing.T) {
	h := newHarness(t).boot()
	in, release := make(chan struct{}), make(chan struct{})
	h.script.set(deploy.StageRollout, entered(in, func(context.Context, *stage.RunCtx) error {
		<-release
		return nil
	}))
	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
	<-in
	_, err := h.eng.Cancel(bg, owner, deploy.RunRequest{RunID: id})
	require.NoError(t, err)
	close(release)

	run := h.await(id, deploy.RunCancelled)

	assert.Equal(t,
		cancelView{[]deploy.StageID{deploy.StagePreflight, deploy.StageACL, deploy.StageRollout}, deploy.StatePending},
		cancelView{h.script.calls(), run.Stage(deploy.StageVerify).State})
}

func TestCancelAbandonsAFailedRun(t *testing.T) {
	h := newHarness(t).boot()
	h.script.set(deploy.StageACL, failOnce(ports.Failf(deploy.FailTimeout, "reload")))
	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
	h.await(id, deploy.RunFailed)

	run, err := h.eng.Cancel(bg, owner, deploy.RunRequest{RunID: id})
	require.NoError(t, err)
	_, resumeErr := h.eng.Resume(bg, owner, deploy.RunRequest{RunID: id})

	assert.Equal(t, deploy.RunCancelled, run.State)
	assert.ErrorIs(t, resumeErr, ports.ErrNotResumable)
}

func TestApproveResumesTheWaitingStage(t *testing.T) {
	h := newHarness(t).boot()
	h.script.set(deploy.StageACL, awaitApproval)
	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID

	waiting := h.until(id, func(r deploy.Run) bool { return r.State == deploy.RunWaiting })
	approved, err := h.eng.Approve(bg, owner, deploy.RunRequest{RunID: id, Stage: deploy.StageACL})
	require.NoError(t, err)

	h.await(id, deploy.RunSucceeded)
	assert.Equal(t,
		[]string{"apply messaging?", string(deploy.RunRunning)},
		[]string{waiting.Stage(deploy.StageACL).NeedsApproval, string(approved.State)})
}

func TestApproveRefusedWhenNothingWaits(t *testing.T) {
	h := newHarness(t).boot()
	in, release := make(chan struct{}), make(chan struct{})
	h.script.set(deploy.StagePreflight, entered(in, func(context.Context, *stage.RunCtx) error {
		<-release
		return nil
	}))
	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
	<-in

	_, err := h.eng.Approve(bg, owner, deploy.RunRequest{RunID: id})
	close(release)

	require.ErrorIs(t, err, ports.ErrNotResumable)
	h.await(id, deploy.RunSucceeded)
}

func TestOneRunHoldsTheCluster(t *testing.T) {
	t.Run("second start refused while a run is active", func(t *testing.T) {
		h := newHarness(t).boot()
		in, release := make(chan struct{}), make(chan struct{})
		h.script.set(deploy.StagePreflight, entered(in, func(context.Context, *stage.RunCtx) error {
			<-release
			return nil
		}))
		first := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
		<-in

		_, err := h.eng.Start(bg, owner, deploy.StartRequest{Kind: deploy.KindReapply})
		close(release)

		require.ErrorIs(t, err, ports.ErrConflict)
		h.await(first, deploy.RunSucceeded)
	})
	t.Run("lock held by another owner", func(t *testing.T) {
		h := newHarness(t).boot()
		h.store.holdLock("other")

		_, err := h.eng.Start(bg, owner, deploy.StartRequest{Kind: deploy.KindReapply})

		require.ErrorIs(t, err, ports.ErrLockHeld)
	})
	t.Run("lock is released when the run ends", func(t *testing.T) {
		h := newHarness(t).boot()
		h.await(h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID, deploy.RunSucceeded)

		h.await(h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID, deploy.RunSucceeded)
	})
}

func TestBootResumesTheActiveRun(t *testing.T) {
	cases := []struct {
		name  string
		lock  deploy.RunID
		state deploy.RunState
		ran   []deploy.StageID
	}{
		{
			name: "own lock is re-acquired", lock: "r1", state: deploy.RunSucceeded,
			ran: []deploy.StageID{deploy.StageACL, deploy.StageRollout, deploy.StageVerify},
		},
		{name: "lock held by another run fails it", lock: "other", state: deploy.RunFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			seedInterrupted(t, h.store)
			h.store.holdLock(tc.lock)

			run := h.boot().await("r1", tc.state)

			assert.Equal(t, tc.ran, h.script.calls())
			assert.Equal(t, tc.state == deploy.RunFailed, run.Failure != nil && run.Failure.Code == deploy.FailLockHeld)
		})
	}
}

// seedInterrupted stores a reapply run a previous pod left mid-acl.
func seedInterrupted(t *testing.T, s *memStore) {
	now := time.Now()
	run := deploy.Run{
		ID: "r1", Kind: deploy.KindReapply, State: deploy.RunRunning, Actor: owner,
		Stages: pendingStages(deploy.KindReapply), CreatedAt: now, UpdatedAt: now,
	}
	run.Stages[0].State = deploy.StateSucceeded
	run.Stages[1].State, run.Stages[1].StartedAt, run.Stages[1].Attempts = deploy.StateRunning, &now, 1
	_, err := s.Put(bg, &run, 0)
	require.NoError(t, err)
}

func TestProgressWritesAreCoalesced(t *testing.T) {
	h := newHarness(t)
	h.eng.progressEvery = time.Hour
	h.script.set(deploy.StageRollout, func(ctx context.Context, rc *stage.RunCtx) error {
		for i := 1; i <= 100; i++ {
			rc.SetProgress(ctx, deploy.Progress{Done: i, Total: 100})
		}
		return nil
	})
	h.boot()

	run := h.await(h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID, deploy.RunSucceeded)

	// create, begin and end of four stages, the ending: ten writes, and the
	// hundred progress reports ride the rollout's end.
	assert.Equal(t,
		progressView{Puts: 10, Rollout: deploy.Progress{Done: 100, Total: 100}},
		progressView{Puts: h.store.putCount(), Rollout: run.Stage(deploy.StageRollout).Progress})
}

type progressView struct {
	Puts    int
	Rollout deploy.Progress
}

func TestGetAndListReadTheRuns(t *testing.T) {
	h := newHarness(t).boot()
	runs, active, err := h.eng.List(bg, owner, deploy.ListRequest{})
	require.NoError(t, err)
	assert.Equal(t, listView{Runs: []deploy.RunSummary{}}, listView{Runs: runs, Active: active})

	id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID
	done := h.await(id, deploy.RunSucceeded)
	got, err := h.eng.Get(bg, owner, deploy.RunRequest{RunID: id})
	require.NoError(t, err)
	runs, active, err = h.eng.List(bg, owner, deploy.ListRequest{Limit: 5})
	require.NoError(t, err)

	assert.Equal(t, listView{Runs: []deploy.RunSummary{done.Summary()}, Got: done}, listView{Runs: runs, Active: active, Got: got})
}

type listView struct {
	Runs   []deploy.RunSummary
	Active deploy.RunID
	Got    deploy.Run
}
