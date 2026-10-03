// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type jobState struct{ status, conclusion string }

var (
	queued    = jobState{"queued", ""}
	running   = jobState{statusInProgress, ""}
	succeeded = jobState{statusCompleted, conclusionOK}
	failed    = jobState{statusCompleted, "failure"}
)

func job(id int64, name string, s jobState) ports.Job {
	return ports.Job{ID: id, Name: name, Status: s.status, Conclusion: s.conclusion, URL: fmt.Sprintf("https://ci/job/%d", id)}
}

func workflowRun(id int64, sha deploy.SHA, s jobState) ports.WorkflowRun {
	number := int(id) + 33
	return ports.WorkflowRun{
		ID: id, Number: number, HeadSHA: sha, Status: s.status, Conclusion: s.conclusion,
		URL: fmt.Sprintf("https://ci/run/%d", id), CreatedAt: time.Unix(int64(number), 0),
	}
}

func withAttempt(r ports.WorkflowRun, n int) ports.WorkflowRun {
	r.Attempt = n
	return r
}

func usersJobs(build, manifest jobState) []ports.Job {
	return []ports.Job{
		job(1, "Select images", succeeded),
		job(2, "Build users Intel x86_64", build),
		job(3, "Build users ARM64", build),
		job(4, "Publish manifest users", manifest),
	}
}

type buildResult struct {
	Done     bool
	Waits    []string
	Progress deploy.Progress
	Items    []deploy.Item
	RunID    int64
}

func TestBuildStageWaitsAndGroupsJobs(t *testing.T) {
	run := newRun(deploy.KindRelease)
	run.Version, run.Outputs.TagSHA = "v0.2.3-beta", "c1"
	f := newFixture(t, run)
	f.gh.runs = []ports.WorkflowRun{workflowRun(8, "c0", running), workflowRun(9, "c1", queued)}
	f.gh.steps[9] = []runStep{
		{run: workflowRun(9, "c1", queued)},
		{run: workflowRun(9, "c1", queued)},
		{run: workflowRun(9, "c1", running), jobs: usersJobs(running, queued)},
		{run: workflowRun(9, "c1", succeeded), jobs: usersJobs(succeeded, succeeded)},
	}

	_, err := f.runStage(t, deploy.StageBuild)

	require.NoError(t, err)
	st := f.stageOf(deploy.StageBuild)
	assert.Equal(t, buildResult{
		Waits:    []string{"queued behind run #41"},
		Progress: deploy.Progress{Done: 4, Total: 4},
		Items: []deploy.Item{
			{Key: "Select images", Label: "Select images", State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 1, Total: 1}, URL: "https://ci/job/1"},
			{Key: "users", Label: "users", State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 3, Total: 3}, URL: "https://ci/job/2"},
		},
		RunID: 9,
	}, buildResult{Waits: f.sink.waits, Progress: st.Progress, Items: st.Items, RunID: f.sink.View().Outputs.BuildRunID})
}

func TestBuildStageWaitsOutTheStaleAttempt(t *testing.T) {
	run := newRun(deploy.KindRelease)
	run.Version, run.Outputs.TagSHA, run.Outputs.BuildRunAttempt = "v0.2.3-beta", "c1", 2
	f := newFixture(t, run)
	stale := withAttempt(workflowRun(9, "c1", failed), 1)
	f.gh.runs = []ports.WorkflowRun{stale}
	f.gh.steps[9] = []runStep{
		{run: stale},
		{run: stale, jobs: usersJobs(failed, queued)},
		{run: withAttempt(workflowRun(9, "c1", succeeded), 2), jobs: usersJobs(succeeded, succeeded)},
	}

	done, err := f.runStage(t, deploy.StageBuild)

	require.NoError(t, err)
	assert.Equal(t,
		buildResult{Waits: []string{"waiting for rerun attempt 2 to start"}, Progress: deploy.Progress{Done: 4, Total: 4}},
		buildResult{Done: done, Waits: f.sink.waits, Progress: f.stageOf(deploy.StageBuild).Progress})
}

func TestBuildStageFailureCarriesTheJobLog(t *testing.T) {
	run := newRun(deploy.KindRelease)
	run.Version, run.Outputs.TagSHA = "v0.2.3-beta", "c1"
	f := newFixture(t, run)
	jobs := usersJobs(succeeded, succeeded)
	jobs[2] = job(3, "Build users ARM64", failed)
	f.gh.runs = []ports.WorkflowRun{workflowRun(9, "c1", running)}
	f.gh.steps[9] = []runStep{{run: f.gh.runs[0]}, {run: workflowRun(9, "c1", failed), jobs: jobs}}
	for i := range 60 {
		f.gh.logs[3] = append(f.gh.logs[3], fmt.Sprintf("line %d", i))
	}

	_, err := f.runStage(t, deploy.StageBuild)

	fl, ok := ports.AsFail(err)
	require.True(t, ok, "err = %v, want a Fail", err)
	assert.Equal(t, deploy.Failure{
		Code: deploy.FailBuildFailed, Message: "publish-images run #42 concluded failure: job Build users ARM64 failed",
		LogTail: f.gh.logs[3][10:], Actions: []deploy.Action{deploy.ActionRerun, deploy.ActionCancel},
	}, fl.Failure)
}

type buildDoneResult struct {
	Done  bool
	URL   string
	Query ports.RunQuery
}

func TestBuildDone(t *testing.T) {
	release := ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "v0.2.3-beta", SHA: "tagged"}
	bump := ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "main", SHA: "target"}
	finished := buildDoneResult{Done: true, URL: "https://ci/run/9"}
	cases := []struct {
		name    string
		kind    deploy.RunKind
		floor   int
		attempt int
		state   jobState
		want    buildDoneResult
	}{
		{"release looks for the tag push and finds a succeeded run", deploy.KindRelease, 0, 1, succeeded, withQuery(finished, release)},
		{"hotfix looks for the tag push and finds a succeeded run", deploy.KindHotfix, 0, 1, succeeded, withQuery(finished, release)},
		{"bump looks for the main push at the target and finds a succeeded run", deploy.KindBump, 0, 1, succeeded, withQuery(finished, bump)},
		{"old attempt succeeded before the rerun", deploy.KindBump, 2, 1, succeeded, buildDoneResult{Query: bump}},
		{"rerun attempt succeeded", deploy.KindBump, 2, 2, succeeded, withQuery(finished, bump)},
		{"rerun attempt failed", deploy.KindBump, 2, 2, failed, buildDoneResult{Query: bump}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(tc.kind)
			run.Version, run.TargetSHA = "v0.2.3-beta", "target"
			run.Outputs.TagSHA, run.Outputs.BuildRunAttempt = "tagged", tc.floor
			f := newFixture(t, run)
			sha := tc.want.Query.SHA
			f.gh.runs = []ports.WorkflowRun{withAttempt(workflowRun(9, sha, tc.state), tc.attempt)}
			f.gh.steps[9] = []runStep{{run: f.gh.runs[0]}}

			done, err := f.stageDone(t.Context(), t, deploy.StageBuild)

			require.NoError(t, err)
			require.Len(t, f.gh.queries, 1)
			assert.Equal(t, tc.want, buildDoneResult{Done: done, URL: f.sink.View().Outputs.BuildRunURL, Query: f.gh.queries[0]})
		})
	}
}

func withQuery(r buildDoneResult, q ports.RunQuery) buildDoneResult {
	r.Query = q
	return r
}
