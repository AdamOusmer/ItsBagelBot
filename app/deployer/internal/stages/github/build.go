// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"fmt"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/redact"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	statusCompleted  = "completed"
	statusInProgress = "in_progress"
	conclusionOK     = "success"
	conclusionSkip   = "skipped"
)

func buildQuery(run deploy.Run, cfg ports.Config) ports.RunQuery {
	q := ports.RunQuery{Workflow: cfg.Workflow, Event: "push", Ref: ports.Ref(run.Version), SHA: run.Outputs.TagSHA}
	if run.Kind == deploy.KindBump {
		q.Ref, q.SHA = mainRef(cfg), run.TargetSHA
	}
	return q
}

func buildDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	wr, found, err := rc.Deps.GitHub.FindWorkflowRun(ctx, buildQuery(rc.View(), rc.Deps.Config))
	if err != nil || !found {
		return false, err
	}
	if !buildSucceeded(wr, rc.View()) {
		return false, nil
	}
	return true, recordBuild(ctx, rc, wr)
}

func buildSucceeded(wr ports.WorkflowRun, run deploy.Run) bool {
	return wr.Status == statusCompleted && wr.Conclusion == conclusionOK && !staleAttempt(wr, run)
}

func staleAttempt(wr ports.WorkflowRun, run deploy.Run) bool {
	return wr.Attempt < run.Outputs.BuildRunAttempt
}

func recordBuild(ctx context.Context, rc *stage.RunCtx, wr ports.WorkflowRun) error {
	rc.AddLink(ctx, deploy.Link{Label: fmt.Sprintf("publish-images #%d", wr.Number), URL: wr.URL})
	return rc.SetOutputs(ctx, func(o *deploy.Outputs) { o.BuildRunID, o.BuildRunURL = wr.ID, wr.URL })
}

func runBuild(ctx context.Context, rc *stage.RunCtx) error {
	w := &buildWatch{rc: rc, q: buildQuery(rc.View(), rc.Deps.Config)}
	if err := poll(ctx, rc, w.find); err != nil {
		return err
	}
	if err := poll(ctx, rc, w.wait); err != nil {
		return err
	}
	rc.Waiting(ctx, "")
	return nil
}

type buildWatch struct {
	rc  *stage.RunCtx
	q   ports.RunQuery
	run ports.WorkflowRun
}

func (w *buildWatch) find(ctx context.Context) (bool, error) {
	wr, found, err := w.rc.Deps.GitHub.FindWorkflowRun(ctx, w.q)
	if err != nil {
		return false, err
	}
	if !found {
		w.rc.Waiting(ctx, fmt.Sprintf("waiting for %s to start for %s at %s", w.q.Workflow, w.q.Ref, short(w.q.SHA)))
		return false, nil
	}
	w.run = wr
	return true, recordBuild(ctx, w.rc, wr)
}

func (w *buildWatch) wait(ctx context.Context) (bool, error) {
	wr, err := w.rc.Deps.GitHub.WorkflowRun(ctx, w.run.ID)
	if err != nil {
		return false, err
	}
	if staleAttempt(wr, w.rc.View()) {
		w.rc.Waiting(ctx, fmt.Sprintf("waiting for rerun attempt %d to start", w.rc.View().Outputs.BuildRunAttempt))
		return false, nil
	}
	jobs, err := w.rc.Deps.GitHub.RunJobs(ctx, wr.ID)
	if err != nil {
		return false, err
	}
	w.run = wr
	w.report(ctx, jobs)
	if wr.Status != statusCompleted {
		return false, w.pending(ctx)
	}
	if wr.Conclusion == conclusionOK {
		return true, nil
	}
	return false, w.failure(ctx, jobs)
}

func (w *buildWatch) report(ctx context.Context, jobs []ports.Job) {
	w.rc.SetItems(ctx, jobItems(jobs))
	w.rc.SetProgress(ctx, deploy.Progress{Done: countCompleted(jobs), Total: len(jobs)})
}

func (w *buildWatch) pending(ctx context.Context) error {
	if w.run.Status == statusInProgress {
		w.rc.Waiting(ctx, "")
		return nil
	}
	active, err := w.rc.Deps.GitHub.ActiveRuns(ctx, w.q.Workflow)
	if err != nil {
		return err
	}
	msg := "queued, waiting for a runner"
	if ahead, ok := runAhead(active, w.run); ok {
		msg = fmt.Sprintf("queued behind run #%d", ahead.Number)
	}
	w.rc.Waiting(ctx, msg)
	return nil
}

func runAhead(active []ports.WorkflowRun, ours ports.WorkflowRun) (ports.WorkflowRun, bool) {
	var ahead ports.WorkflowRun
	found := false
	for _, r := range active {
		if r.ID != ours.ID && r.CreatedAt.Before(ours.CreatedAt) {
			ahead, found = r, true
		}
	}
	return ahead, found
}

func (w *buildWatch) failure(ctx context.Context, jobs []ports.Job) error {
	f := stage.Failf(deploy.FailBuildFailed, "publish-images run #%d concluded %s", w.run.Number, w.run.Conclusion)
	f.Actions = []deploy.Action{deploy.ActionRerun, deploy.ActionCancel}
	job, ok := failedJob(jobs)
	if !ok {
		return f
	}
	f.Message += ": job " + job.Name + " failed"
	tail, err := w.rc.Deps.GitHub.JobLogTail(ctx, job.ID, w.rc.Deps.Config.LogTailLines)
	if err != nil {
		tail = []string{"log unavailable: " + err.Error()}
	}
	f.LogTail = redact.Lines(tail)
	return f
}

func failedJob(jobs []ports.Job) (ports.Job, bool) {
	for _, j := range jobs {
		if jobFailed(j) {
			return j, true
		}
	}
	return ports.Job{}, false
}

func jobFailed(j ports.Job) bool {
	return j.Status == statusCompleted && !okConclusion(j.Conclusion)
}

func okConclusion(c string) bool { return c == conclusionOK || c == conclusionSkip }

func countCompleted(jobs []ports.Job) int {
	n := 0
	for _, j := range jobs {
		if j.Status == statusCompleted {
			n++
		}
	}
	return n
}

// Job name prefixes must match publish-images.yml.
const (
	buildPrefix    = "Build "
	manifestPrefix = "Publish manifest "
)

var archLabels = []string{" Intel x86_64", " ARM64"}

func jobImage(name string) (deploy.ImageName, bool) {
	if img, ok := strings.CutPrefix(name, manifestPrefix); ok {
		return deploy.ImageName(img), true
	}
	rest, ok := strings.CutPrefix(name, buildPrefix)
	if !ok {
		return "", false
	}
	for _, arch := range archLabels {
		rest = strings.TrimSuffix(rest, arch)
	}
	return deploy.ImageName(rest), true
}

func jobKey(j ports.Job) string {
	if img, ok := jobImage(j.Name); ok {
		return string(img)
	}
	return j.Name
}

func jobItems(jobs []ports.Job) []deploy.Item {
	var order []string
	groups := map[string][]ports.Job{}
	for _, j := range jobs {
		key := jobKey(j)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], j)
	}
	items := make([]deploy.Item, 0, len(order))
	for _, key := range order {
		items = append(items, groupItem(key, groups[key]))
	}
	return items
}

func groupItem(key string, jobs []ports.Job) deploy.Item {
	return deploy.Item{
		Key: key, Label: key, State: groupState(jobs), URL: jobs[0].URL,
		Progress: deploy.Progress{Done: countCompleted(jobs), Total: len(jobs)},
	}
}

func groupState(jobs []ports.Job) deploy.StageState {
	if _, failed := failedJob(jobs); failed {
		return deploy.StateFailed
	}
	switch countCompleted(jobs) {
	case len(jobs):
		return deploy.StateSucceeded
	case 0:
		return startedState(jobs)
	}
	return deploy.StateRunning
}

func startedState(jobs []ports.Job) deploy.StageState {
	for _, j := range jobs {
		if j.Status == statusInProgress {
			return deploy.StateRunning
		}
	}
	return deploy.StatePending
}

func builtImages(jobs []ports.Job) []deploy.ImageName {
	var out []deploy.ImageName
	for _, j := range jobs {
		img, ok := strings.CutPrefix(j.Name, manifestPrefix)
		if ok && j.Conclusion == conclusionOK {
			out = append(out, deploy.ImageName(img))
		}
	}
	return out
}
