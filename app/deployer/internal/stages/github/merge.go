// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func mergePRsDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	run := rc.View()
	if len(run.PRs) == 0 {
		return false, nil
	}
	for _, n := range run.PRs {
		pr, err := rc.Deps.GitHub.PR(ctx, n)
		if err != nil || !pr.Merged {
			return false, err
		}
		if err := includeMerge(ctx, rc, pr); err != nil {
			return false, err
		}
	}
	return true, rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.MergedPRs = append([]int(nil), run.PRs...) })
}

func runMergePRs(ctx context.Context, rc *stage.RunCtx) error {
	run := rc.View()
	if len(run.PRs) == 0 {
		return stage.ErrSkipped
	}
	items := make([]deploy.Item, len(run.PRs))
	for i, n := range run.PRs {
		items[i] = deploy.Item{Key: prKey(n), Label: "#" + strconv.Itoa(n), State: deploy.StatePending}
	}
	rc.SetItems(ctx, items)
	for i, n := range run.PRs {
		if rc.Cancelled() {
			return stage.ErrCancelled
		}
		if err := mergeAndRecord(ctx, rc, n); err != nil {
			return err
		}
		rc.SetProgress(ctx, deploy.Progress{Done: i + 1, Total: len(run.PRs)})
	}
	return nil
}

func mergeAndRecord(ctx context.Context, rc *stage.RunCtx, number int) error {
	pr, err := mergePR(ctx, rc, number)
	if err != nil {
		return err
	}
	if err := includeMerge(ctx, rc, pr); err != nil {
		return err
	}
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.MergedPRs = appendUniqueInt(o.MergedPRs, number) })
}

func includeMerge(ctx context.Context, rc *stage.RunCtx, pr ports.PullRequest) error {
	cur := rc.View().TargetSHA
	if cur != "" {
		cmp, err := rc.Deps.GitHub.Compare(ctx, ports.Ref(cur), ports.Ref(pr.MergeSHA))
		if err != nil || cmp.AheadBy == 0 {
			return err
		}
	}
	return rc.Update(ctx, func(r *deploy.Run) { r.TargetSHA = pr.MergeSHA })
}

func mergePR(ctx context.Context, rc *stage.RunCtx, number int) (ports.PullRequest, error) {
	m := &merger{rc: rc, number: number}
	if err := poll(ctx, rc, m.step); err != nil {
		m.report(ctx, stoppedState(err), err.Error())
		return ports.PullRequest{}, err
	}
	m.report(ctx, deploy.StateSucceeded, "merged as "+short(m.pr.MergeSHA))
	rc.Waiting(ctx, "")
	return m.pr, nil
}

type merger struct {
	rc          *stage.RunCtx
	number      int
	pr          ports.PullRequest
	updates     int
	updatedFrom deploy.SHA
}

func (m *merger) step(ctx context.Context) (bool, error) {
	pr, err := m.rc.Deps.GitHub.PR(ctx, m.number)
	if err != nil {
		return false, err
	}
	m.pr = pr
	if pr.Merged {
		return true, m.deleteBranch(ctx)
	}
	if err := refuseUnmergeable(pr); err != nil {
		return false, err
	}
	if pr.Behind {
		return false, m.updateBranch(ctx)
	}
	green, err := m.checksGreen(ctx)
	if err != nil || !green {
		return false, err
	}
	return m.merge(ctx)
}

func refuseUnmergeable(pr ports.PullRequest) error {
	switch {
	case !pr.Open:
		return fail(deploy.FailNotMergeable, "PR #%d was closed without merging", pr.Number)
	case pr.Draft:
		return fail(deploy.FailNotMergeable, "PR #%d is a draft", pr.Number)
	case pr.Mergeable != nil && !*pr.Mergeable:
		return fail(deploy.FailNotMergeable, "PR #%d has merge conflicts with %s", pr.Number, pr.BaseBranch)
	}
	return nil
}

func (m *merger) updateBranch(ctx context.Context) error {
	if m.pr.HeadSHA == m.updatedFrom {
		m.report(ctx, deploy.StateWaiting, "updating branch with main")
		return nil
	}
	limit := m.rc.Deps.Config.UpdateBranchLimit
	if m.updates >= limit {
		return fail(deploy.FailBehindLimit, "PR #%d is still behind main after %d branch updates", m.number, limit)
	}
	if err := m.rc.Deps.GitHub.UpdateBranch(ctx, m.number); err != nil {
		return err
	}
	m.updates++
	m.updatedFrom = m.pr.HeadSHA
	m.report(ctx, deploy.StateWaiting, fmt.Sprintf("behind main, branch updated (%d/%d)", m.updates, limit))
	return nil
}

func (m *merger) checksGreen(ctx context.Context) (bool, error) {
	sum, err := m.rc.Deps.GitHub.Checks(ctx, m.pr.HeadSHA)
	if err != nil {
		return false, err
	}
	switch {
	case sum.State == deploy.ChecksFailure || sum.CodeScene == deploy.ChecksFailure:
		return false, checksFailed(m.pr, sum)
	case sum.State == deploy.ChecksSuccess && sum.CodeScene == deploy.ChecksSuccess:
		return true, nil
	}
	m.report(ctx, deploy.StateWaiting, "waiting for required checks")
	return false, nil
}

func checksFailed(pr ports.PullRequest, sum ports.CheckSummary) *stage.Fail {
	var failed []string
	for _, c := range sum.Checks {
		if c.State == deploy.ChecksFailure {
			failed = append(failed, c.Evidence()...)
		}
	}
	f := fail(deploy.FailChecksFailed, "PR #%d: required checks failed", pr.Number)
	f.LogTail = failed
	return f
}

func (m *merger) merge(ctx context.Context) (bool, error) {
	if m.pr.Mergeable == nil {
		m.report(ctx, deploy.StateWaiting, "GitHub is computing mergeability")
		return false, nil
	}
	m.report(ctx, deploy.StateRunning, "merging")
	sha, err := m.rc.Deps.GitHub.SquashMerge(ctx, m.number, m.pr.HeadSHA)
	if errors.Is(err, ports.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	m.pr.Merged, m.pr.MergeSHA = true, sha
	return true, m.deleteBranch(ctx)
}

func (m *merger) deleteBranch(ctx context.Context) error {
	err := m.rc.Deps.GitHub.DeleteBranch(ctx, m.pr.HeadBranch)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	return err
}

func (m *merger) report(ctx context.Context, state deploy.StageState, detail string) {
	label := "#" + strconv.Itoa(m.number)
	if m.pr.Title != "" {
		label += " " + m.pr.Title
	}
	m.rc.SetItem(ctx, deploy.Item{Key: prKey(m.number), Label: label, State: state, Detail: detail, URL: m.pr.URL})
	if state == deploy.StateWaiting {
		m.rc.Waiting(ctx, "PR #"+strconv.Itoa(m.number)+": "+detail)
	}
}

func stoppedState(err error) deploy.StageState {
	if errors.Is(err, stage.ErrCancelled) {
		return deploy.StateCancelled
	}
	return deploy.StateFailed
}

func prKey(n int) string { return "pr-" + strconv.Itoa(n) }

func appendUniqueInt(list []int, n int) []int {
	for _, have := range list {
		if have == n {
			return list
		}
	}
	return append(append([]int(nil), list...), n)
}

func joinList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
