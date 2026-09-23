// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type endView struct {
	State    deploy.RunState
	Failure  bool
	LockHeld bool
}

// TestStoreFaultsDoNotStrandTheRun pins that a write whose ack was lost
// (it landed) is adopted rather than read as a takeover, that a heartbeat
// whose ack was lost does not fail the run, and that an ending write the
// hub refuses for a while is retried until it lands. Each fault is armed
// from inside verify, the last stage, so it hits the writes that settle and
// end the run.
func TestStoreFaultsDoNotStrandTheRun(t *testing.T) {
	cases := []struct {
		name  string
		fault func(*memStore)
	}{
		{"lost run write ack", func(s *memStore) { s.lostAcks = 1 }},
		{"lost heartbeat ack", func(s *memStore) { s.lostBeatAcks = 1 }},
		{"hub refuses the ending writes", func(s *memStore) { s.downPuts = 3 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).boot()
			h.script.set(deploy.StageVerify, func(ctx context.Context, _ *stage.RunCtx) error {
				h.store.inject(tc.fault)
				beats := h.store.beatCount()
				for h.store.beatCount() < beats+3 {
					if err := sleepCtx(ctx, time.Millisecond); err != nil {
						return err
					}
				}
				return nil
			})
			id := h.start(deploy.StartRequest{Kind: deploy.KindReapply}).ID

			run := h.await(id, deploy.RunSucceeded)

			_, held, err := h.store.LockOwner(bg)
			require.NoError(t, err)
			assert.Equal(t, endView{State: deploy.RunSucceeded}, endView{run.State, run.Failure != nil, held})
		})
	}
}

// TestOrphanedRunCanBeCancelledOrResumed covers an active run no process
// drives and whose lock expired: its ending write never landed before the
// execution stopped. Both verbs used to refuse it as "not stopped".
func TestOrphanedRunCanBeCancelledOrResumed(t *testing.T) {
	cases := []struct {
		name string
		verb func(*Engine, deploy.RunRequest) (deploy.Run, error)
		want deploy.RunState
	}{
		{"cancel abandons it", func(e *Engine, r deploy.RunRequest) (deploy.Run, error) { return e.Cancel(bg, owner, r) }, deploy.RunCancelled},
		{"resume drives it again", func(e *Engine, r deploy.RunRequest) (deploy.Run, error) { return e.Resume(bg, owner, r) }, deploy.RunSucceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t).boot()
			<-h.eng.ready // seeded after boot, so resumeActive never sees it
			seedInterrupted(t, h.store)

			_, err := tc.verb(h.eng, deploy.RunRequest{RunID: "r1"})
			require.NoError(t, err)

			assert.Equal(t, tc.want, h.await("r1", tc.want).State)
		})
	}
}

// TestDrivenRunIsNotOrphaned pins the other side: a run whose lock another
// process still heartbeats is refused, since that process drives it.
func TestDrivenRunIsNotOrphaned(t *testing.T) {
	h := newHarness(t).boot()
	<-h.eng.ready
	seedInterrupted(t, h.store)
	h.store.holdLock("r1")

	_, err := h.eng.Cancel(bg, owner, deploy.RunRequest{RunID: "r1"})

	assert.ErrorIs(t, err, ports.ErrNotResumable)
}

// TestRestartMidCancelFinishesTheInFlightRollout: the cancel landed while
// rollout had a service in flight and the pod restarted before rollout
// reached its safe point. The next driver runs rollout once more, with the
// cancel already signalled, instead of concluding and leaving the service
// rolling unwatched.
func TestRestartMidCancelFinishesTheInFlightRollout(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	run := deploy.Run{
		ID: "r1", Kind: deploy.KindReapply, State: deploy.RunRunning, Actor: owner, CancelRequested: true,
		Stages: pendingStages(deploy.KindReapply), CreatedAt: now, UpdatedAt: now,
	}
	run.Stages[0].State, run.Stages[1].State = deploy.StateSucceeded, deploy.StateSucceeded
	run.Stages[2].State, run.Stages[2].StartedAt, run.Stages[2].Attempts = deploy.StateRunning, &now, 1
	_, err := h.store.Put(bg, &run, 0)
	require.NoError(t, err)
	sawCancel := false
	h.script.set(deploy.StageRollout, func(_ context.Context, rc *stage.RunCtx) error {
		sawCancel = rc.Cancelled()
		return stage.ErrCancelled
	})

	got := h.boot().await("r1", deploy.RunCancelled)

	assert.Equal(t,
		cancelView{[]deploy.StageID{deploy.StageRollout}, deploy.StateCancelled},
		cancelView{h.script.calls(), got.Stage(deploy.StageRollout).State})
	assert.True(t, sawCancel)
}
