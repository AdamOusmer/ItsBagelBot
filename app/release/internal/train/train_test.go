// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package train_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/stage"
	"ItsBagelBot/app/release/internal/train"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type fakeStage struct {
	id   deploy.StageID
	err  error
	runs *[]deploy.StageID
}

func (s fakeStage) ID() deploy.StageID                                { return s.id }
func (s fakeStage) Done(context.Context, *stage.RunCtx) (bool, error) { return false, nil }
func (s fakeStage) Run(context.Context, *stage.RunCtx) error {
	*s.runs = append(*s.runs, s.id)
	return s.err
}

func stages(runs *[]deploy.StageID, failPromote error) []stage.Stage {
	return []stage.Stage{
		fakeStage{id: deploy.StagePreflight, runs: runs},
		fakeStage{id: deploy.StagePromote, runs: runs, err: failPromote},
	}
}

func states(r deploy.Run) []deploy.StageState {
	var out []deploy.StageState
	for _, s := range r.Stages {
		out = append(out, s.State)
	}
	return out
}

func deps() stage.Deps { return stage.Deps{Log: zap.NewNop()} }

func TestDriveRunsEveryStageAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	f, err := train.Open(path, deploy.Run{Kind: deploy.KindReapply})
	require.NoError(t, err)
	var runs []deploy.StageID

	require.NoError(t, train.New(deps(), stages(&runs, nil)).Drive(t.Context(), f, ""))

	reopened, err := train.Open(path, deploy.Run{})
	require.NoError(t, err)
	assert.Equal(t, []deploy.StageID{deploy.StagePreflight, deploy.StagePromote}, runs)
	assert.Equal(t, deploy.RunSucceeded, reopened.View().State)
	assert.Equal(t, []deploy.StageState{deploy.StateSucceeded, deploy.StateSucceeded}, states(reopened.View()))
}

func TestDriveStopsAtAFailureAndARerunResumesThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	f, err := train.Open(path, deploy.Run{Kind: deploy.KindReapply})
	require.NoError(t, err)
	var runs []deploy.StageID

	err = train.New(deps(), stages(&runs, ports.Failf(deploy.FailGitHub, "boom"))).Drive(t.Context(), f, "")

	require.Error(t, err)
	failed := f.View()
	assert.Equal(t, deploy.RunFailed, failed.State)
	assert.Equal(t, deploy.FailGitHub, failed.Failure.Code)
	assert.Equal(t, []deploy.StageState{deploy.StateSucceeded, deploy.StateFailed}, states(failed))

	runs = nil
	resumed, err := train.Open(path, deploy.Run{})
	require.NoError(t, err)
	require.NoError(t, train.New(deps(), stages(&runs, nil)).Drive(t.Context(), resumed, ""))
	assert.Equal(t, []deploy.StageID{deploy.StagePromote}, runs, "the rerun skips the stage that already succeeded")
}

func TestDriveOneStagePerJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	f, err := train.Open(path, deploy.Run{Kind: deploy.KindReapply})
	require.NoError(t, err)
	var runs []deploy.StageID
	tr := train.New(deps(), stages(&runs, nil))

	require.NoError(t, tr.Drive(t.Context(), f, deploy.StagePreflight))
	assert.Equal(t, deploy.RunRunning, f.View().State, "the run is not done while promote is pending")

	require.NoError(t, tr.Drive(t.Context(), f, deploy.StagePromote))
	assert.Equal(t, []deploy.StageID{deploy.StagePreflight, deploy.StagePromote}, runs)
	assert.Equal(t, deploy.RunSucceeded, f.View().State)
}
