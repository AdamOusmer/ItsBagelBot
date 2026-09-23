// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"errors"
	"reflect"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// outcome names a stage error for table comparison: the Fail code, or a
// pseudo-code for the contract sentinels.
func outcome(t *testing.T, err error) deploy.FailureCode {
	t.Helper()
	if f, ok := ports.AsFail(err); ok {
		return f.Code
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, stage.ErrSkipped):
		return "skipped"
	case errors.Is(err, ports.ErrInvalid):
		return "invalid"
	}
	t.Fatalf("unexpected error: %v", err)
	return ""
}

func ptr[T any](v T) *T { return &v }

func openPR(number int, head deploy.SHA) fakePR {
	return fakePR{PullRequest: ports.PullRequest{
		Number: number, Title: "feat: x", URL: "https://github.test/pull/5", HeadBranch: "feat/x",
		HeadSHA: head, BaseBranch: "main", Open: true, Mergeable: ptr(true),
	}}
}

type mergeResult struct {
	Done   bool
	Code   deploy.FailureCode
	Calls  []string
	Target deploy.SHA
	Merged []int
}

func TestMergePRs(t *testing.T) {
	green := ports.CheckSummary{State: deploy.ChecksSuccess, CodeScene: deploy.ChecksSuccess}
	cases := []struct {
		name   string
		pr     func(fakePR) fakePR
		checks []ports.CheckSummary
		want   mergeResult
	}{
		{
			name: "merges when green and deletes the branch",
			want: mergeResult{Calls: []string{"merge #5", "delete feat/x"}, Target: "merge5", Merged: []int{5}},
		},
		{
			name: "brings a behind branch up to date first",
			pr:   func(p fakePR) fakePR { p.behind, p.Behind = 1, true; return p },
			want: mergeResult{Calls: []string{"update-branch #5", "merge #5", "delete feat/x"}, Target: "merge5", Merged: []int{5}},
		},
		{
			name:   "waits for CodeScene",
			checks: []ports.CheckSummary{{State: deploy.ChecksSuccess, CodeScene: deploy.ChecksPending}, green},
			want:   mergeResult{Calls: []string{"merge #5", "delete feat/x"}, Target: "merge5", Merged: []int{5}},
		},
		{
			name: "stops after the update-branch limit",
			pr:   func(p fakePR) fakePR { p.behind, p.Behind = 99, true; return p },
			want: mergeResult{Code: deploy.FailBehindLimit, Calls: []string{"update-branch #5", "update-branch #5", "update-branch #5"}, Target: "base"},
		},
		{
			name:   "fails on a red required check",
			checks: []ports.CheckSummary{{State: deploy.ChecksFailure, CodeScene: deploy.ChecksSuccess}},
			want:   mergeResult{Code: deploy.FailChecksFailed, Target: "base"},
		},
		{
			name: "refuses a draft",
			pr:   func(p fakePR) fakePR { p.Draft = true; return p },
			want: mergeResult{Code: deploy.FailNotMergeable, Target: "base"},
		},
		{
			name: "refuses a conflicted PR",
			pr:   func(p fakePR) fakePR { p.Mergeable = ptr(false); return p },
			want: mergeResult{Code: deploy.FailNotMergeable, Target: "base"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindBump)
			run.PRs, run.TargetSHA = []int{5}, "base"
			f := newFixture(t, run)
			pr := openPR(5, "h5")
			if tc.pr != nil {
				pr = tc.pr(pr)
			}
			f.gh.addPR(pr)
			f.gh.branches["feat/x"] = "h5"
			f.gh.checks["h5"] = tc.checks
			done, err := f.runStage(t, deploy.StageMergePRs)
			got := f.sink.View()
			res := mergeResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, Target: got.TargetSHA, Merged: got.Outputs.MergedPRs}
			if !reflect.DeepEqual(res, tc.want) {
				t.Errorf("result = %+v, want %+v", res, tc.want)
			}
		})
	}
}

// TestMergePRsDone: a PR merged before the run is done without a call, and
// only moves the target forward.
func TestMergePRsDone(t *testing.T) {
	cases := []struct {
		name     string
		target   deploy.SHA
		mergeSHA deploy.SHA
		want     mergeResult
	}{
		{"merge ahead of the target moves it", "base", "c2", mergeResult{Done: true, Target: "c2", Merged: []int{5}}},
		{"merge behind the target leaves it", "c2", "c1", mergeResult{Done: true, Target: "c2", Merged: []int{5}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindBump)
			run.PRs, run.TargetSHA = []int{5}, tc.target
			f := newFixture(t, run)
			f.gh.commitMain("c1", nil)
			f.gh.commitMain("c2", nil)
			pr := openPR(5, "h5")
			pr.Open, pr.Merged, pr.MergeSHA = false, true, tc.mergeSHA
			f.gh.addPR(pr)
			done, err := f.runStage(t, deploy.StageMergePRs)
			got := f.sink.View()
			res := mergeResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, Target: got.TargetSHA, Merged: got.Outputs.MergedPRs}
			if !reflect.DeepEqual(res, tc.want) {
				t.Errorf("result = %+v, want %+v", res, tc.want)
			}
		})
	}
}

func TestMergePRsSkipsWithoutPRs(t *testing.T) {
	f := newFixture(t, newRun(deploy.KindBump))
	_, err := f.runStage(t, deploy.StageMergePRs)
	if code := outcome(t, err); code != "skipped" {
		t.Errorf("outcome = %q, want skipped", code)
	}
}

func TestMergePRsFailureNamesTheRedChecks(t *testing.T) {
	run := newRun(deploy.KindBump)
	run.PRs = []int{5}
	f := newFixture(t, run)
	f.gh.addPR(openPR(5, "h5"))
	f.gh.checks["h5"] = []ports.CheckSummary{{State: deploy.ChecksFailure, CodeScene: deploy.ChecksFailure, Checks: []ports.Check{
		{Name: "go test", State: deploy.ChecksFailure, URL: "https://ci/1"},
		{Name: "lint", State: deploy.ChecksSuccess, URL: "https://ci/2"},
		{Name: "CodeScene Code Health Review (main)", State: deploy.ChecksFailure, URL: "https://ci/3",
			Summary: "Code Health 9.1\nBumpy Road: merge.go run\n"},
	}}}
	_, err := f.runStage(t, deploy.StageMergePRs)
	fl, _ := ports.AsFail(err)
	want := []string{"go test: https://ci/1", "CodeScene Code Health Review (main): https://ci/3",
		"Code Health 9.1", "Bumpy Road: merge.go run"}
	if fl == nil || !reflect.DeepEqual(fl.LogTail, want) {
		t.Errorf("failure = %+v, want log tail %v", fl, want)
	}
}
