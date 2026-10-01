// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

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
	Tail   []string
}

func withOpenPR(edit func(*fakePR), checks ...ports.CheckSummary) func(*fixture) {
	return func(f *fixture) {
		f.sink.run.PRs, f.sink.run.TargetSHA = []int{5}, "base"
		pr := openPR(5, "h5")
		if edit != nil {
			edit(&pr)
		}
		f.gh.addPR(pr)
		f.gh.branches["feat/x"] = "h5"
		f.gh.checks["h5"] = checks
	}
}

func withMergedPR(target, mergeSHA deploy.SHA) func(*fixture) {
	return func(f *fixture) {
		f.sink.run.PRs, f.sink.run.TargetSHA = []int{5}, target
		f.gh.commitMain("c1", nil)
		f.gh.commitMain("c2", nil)
		pr := openPR(5, "h5")
		pr.Open, pr.Merged, pr.MergeSHA = false, true, mergeSHA
		f.gh.addPR(pr)
	}
}

func TestMergePRs(t *testing.T) {
	green := ports.CheckSummary{State: deploy.ChecksSuccess, CodeScene: deploy.ChecksSuccess}
	merged := mergeResult{Calls: []string{"merge #5", "delete feat/x"}, Target: "merge5", Merged: []int{5}}
	cases := []struct {
		name  string
		setup func(*fixture)
		want  mergeResult
	}{
		{"merges when green and deletes the branch", withOpenPR(nil), merged},
		{
			"brings a behind branch up to date first",
			withOpenPR(func(p *fakePR) { p.behind, p.Behind = 1, true }),
			mergeResult{Calls: []string{"update-branch #5", "merge #5", "delete feat/x"}, Target: "merge5", Merged: []int{5}},
		},
		{
			"waits for CodeScene",
			withOpenPR(nil, ports.CheckSummary{State: deploy.ChecksSuccess, CodeScene: deploy.ChecksPending}, green),
			merged,
		},
		{
			"stops after the update-branch limit",
			withOpenPR(func(p *fakePR) { p.behind, p.Behind = 99, true }),
			mergeResult{Code: deploy.FailBehindLimit, Calls: []string{"update-branch #5", "update-branch #5", "update-branch #5"}, Target: "base"},
		},
		{
			"fails on a red required check",
			withOpenPR(nil, ports.CheckSummary{State: deploy.ChecksFailure, CodeScene: deploy.ChecksSuccess}),
			mergeResult{Code: deploy.FailChecksFailed, Target: "base"},
		},
		{
			"failure names the red checks with their links and summaries",
			withOpenPR(nil, ports.CheckSummary{State: deploy.ChecksFailure, CodeScene: deploy.ChecksFailure, Checks: []ports.Check{
				{Name: "go test", State: deploy.ChecksFailure, URL: "https://ci/1"},
				{Name: "lint", State: deploy.ChecksSuccess, URL: "https://ci/2"},
				{Name: "CodeScene Code Health Review (main)", State: deploy.ChecksFailure, URL: "https://ci/3",
					Summary: "Code Health 9.1\nBumpy Road: merge.go run\n"},
			}}),
			mergeResult{Code: deploy.FailChecksFailed, Target: "base", Tail: []string{
				"go test: https://ci/1", "CodeScene Code Health Review (main): https://ci/3",
				"Code Health 9.1", "Bumpy Road: merge.go run",
			}},
		},
		{"refuses a draft", withOpenPR(func(p *fakePR) { p.Draft = true }), mergeResult{Code: deploy.FailNotMergeable, Target: "base"}},
		{
			"refuses a conflicted PR",
			withOpenPR(func(p *fakePR) { p.Mergeable = ptr(false) }),
			mergeResult{Code: deploy.FailNotMergeable, Target: "base"},
		},
		{
			"merge ahead of the target moves it",
			withMergedPR("base", "c2"),
			mergeResult{Done: true, Target: "c2", Merged: []int{5}},
		},
		{
			"merge behind the target leaves it",
			withMergedPR("c2", "c1"),
			mergeResult{Done: true, Target: "c2", Merged: []int{5}},
		},
		{"skips when the run has no PRs", func(*fixture) {}, mergeResult{Code: "skipped"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, newRun(deploy.KindBump))
			tc.setup(f)

			done, err := f.runStage(t, deploy.StageMergePRs)

			run := f.sink.View()
			res := mergeResult{Done: done, Code: outcome(t, err), Calls: f.gh.calls, Target: run.TargetSHA, Merged: run.Outputs.MergedPRs}
			if fl, ok := ports.AsFail(err); ok {
				res.Tail = fl.LogTail
			}
			assert.Equal(t, tc.want, res)
		})
	}
}
