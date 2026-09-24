// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"fmt"
	"reflect"
	"testing"
	"time"

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

func usersJobs(build, manifest jobState) []ports.Job {
	return []ports.Job{
		job(1, "Select images", succeeded),
		job(2, "Build users Intel x86_64", build),
		job(3, "Build users ARM64", build),
		job(4, "Publish manifest users", manifest),
	}
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
	if _, err := f.runStage(t, deploy.StageBuild); err != nil {
		t.Fatal(err)
	}
	st := f.stageOf(deploy.StageBuild)
	got := buildResult{Waits: f.sink.waits, Progress: st.Progress, Items: st.Items, RunID: f.sink.View().Outputs.BuildRunID}
	want := buildResult{
		Waits:    []string{"queued behind run #41"},
		Progress: deploy.Progress{Done: 4, Total: 4},
		Items: []deploy.Item{
			{Key: "Select images", Label: "Select images", State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 1, Total: 1}, URL: "https://ci/job/1"},
			{Key: "users", Label: "users", State: deploy.StateSucceeded, Progress: deploy.Progress{Done: 3, Total: 3}, URL: "https://ci/job/2"},
		},
		RunID: 9,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("result = %+v\nwant %+v", got, want)
	}
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
	got := staleResult{Done: done, Waits: f.sink.waits, Progress: f.stageOf(deploy.StageBuild).Progress}
	want := staleResult{Waits: []string{"waiting for rerun attempt 2 to start"}, Progress: deploy.Progress{Done: 4, Total: 4}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("result = %+v (err %v)\nwant %+v", got, err, want)
	}
}

type staleResult struct {
	Done     bool
	Waits    []string
	Progress deploy.Progress
}

func withAttempt(r ports.WorkflowRun, n int) ports.WorkflowRun {
	r.Attempt = n
	return r
}

func TestBuildSucceededHonorsTheRerunFloor(t *testing.T) {
	cases := []struct {
		name  string
		run   ports.WorkflowRun
		floor int
		want  bool
	}{
		{"first attempt succeeded", withAttempt(workflowRun(9, "c1", succeeded), 1), 0, true},
		{"old attempt succeeded before the rerun", withAttempt(workflowRun(9, "c1", succeeded), 1), 2, false},
		{"rerun attempt succeeded", withAttempt(workflowRun(9, "c1", succeeded), 2), 2, true},
		{"rerun attempt failed", withAttempt(workflowRun(9, "c1", failed), 2), 2, false},
	}
	for _, tc := range cases {
		run := deploy.Run{Outputs: deploy.Outputs{BuildRunAttempt: tc.floor}}
		if got := buildSucceeded(tc.run, run); got != tc.want {
			t.Errorf("%s: succeeded = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type buildResult struct {
	Waits    []string
	Progress deploy.Progress
	Items    []deploy.Item
	RunID    int64
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
	if !ok {
		t.Fatalf("err = %v, want a Fail", err)
	}
	got := fl.Failure
	want := deploy.Failure{
		Code: deploy.FailBuildFailed, Message: "publish-images run #42 concluded failure: job Build users ARM64 failed",
		LogTail: f.gh.logs[3][10:], Actions: []deploy.Action{deploy.ActionRerun, deploy.ActionCancel},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("failure = %+v\nwant %+v", got, want)
	}
}

func TestBuildQuery(t *testing.T) {
	cases := []struct {
		kind deploy.RunKind
		want ports.RunQuery
	}{
		{deploy.KindRelease, ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "v0.2.3-beta", SHA: "tagged"}},
		{deploy.KindHotfix, ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "v0.2.3-beta", SHA: "tagged"}},
		{deploy.KindBump, ports.RunQuery{Workflow: "publish-images.yml", Event: "push", Ref: "main", SHA: "target"}},
	}
	for _, tc := range cases {
		run := deploy.Run{Kind: tc.kind, Version: "v0.2.3-beta", TargetSHA: "target", Outputs: deploy.Outputs{TagSHA: "tagged"}}
		if got := buildQuery(run, testConfig()); got != tc.want {
			t.Errorf("%s: query = %+v, want %+v", tc.kind, got, tc.want)
		}
	}
}

func TestBuildDoneOnASucceededRun(t *testing.T) {
	run := newRun(deploy.KindBump)
	run.TargetSHA = "target"
	f := newFixture(t, run)
	f.gh.runs = []ports.WorkflowRun{workflowRun(9, "target", succeeded)}
	f.gh.steps[9] = []runStep{{run: f.gh.runs[0]}}
	done, err := f.runStage(t, deploy.StageBuild)
	got := [2]any{done, f.sink.View().Outputs.BuildRunURL}
	if want := [2]any{true, "https://ci/run/9"}; err != nil || got != want {
		t.Errorf("done, url = %v (err %v), want %v", got, err, want)
	}
}

func TestJobImage(t *testing.T) {
	cases := map[string]deploy.ImageName{
		"Build console-admin Intel x86_64": "console-admin",
		"Build console-admin ARM64":        "console-admin",
		"Publish manifest console-admin":   "console-admin",
		"Select images":                    "",
	}
	for name, want := range cases {
		if got, _ := jobImage(name); got != want {
			t.Errorf("jobImage(%q) = %q, want %q", name, got, want)
		}
	}
}
