// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// preflight gates the train on the cluster and the target commit. The
// cluster-wide lock is the engine's: it is held before any stage runs.
type preflight struct{}

func (preflight) ID() deploy.StageID { return deploy.StagePreflight }

// Done is always false: every check is about the world right now, and the
// world an earlier attempt checked has moved on.
func (preflight) Done(context.Context, *stage.RunCtx) (bool, error) { return false, nil }

func (preflight) Run(ctx context.Context, rc *stage.RunCtx) error {
	p := &checkRun{rc: rc}
	// Settled runs last: the checks wait can take minutes, and settled must
	// describe the cluster the train is about to touch, not the one before it.
	return runSteps(ctx, rc, []step{
		{key: "cluster", label: "Cluster reachable", run: p.reachable},
		{key: "lint", label: "Manifest lint", run: p.lint},
		{key: "checks", label: "Required checks green", run: p.checks},
		{key: "settled", label: "Workloads settled", run: p.settled},
	})
}

// checkRun carries what one preflight's checks share.
type checkRun struct {
	rc      *stage.RunCtx
	target  deploy.SHA
	objs    ports.Objects
	waiting string
}

func (p *checkRun) reachable(ctx context.Context) error {
	if err := p.rc.Deps.Watcher.Reachable(ctx); err != nil {
		return ports.Failf(deploy.FailClusterUnreachable, "cluster unreachable: %v", err)
	}
	return nil
}

// lint builds the target commit's manifests and refuses a one-pod-per-node
// workload that surges: on a cluster with a pod of it on every node the
// surge pod has nowhere to schedule and the rollout deadlocks.
func (p *checkRun) lint(ctx context.Context) error {
	target, err := resolveTarget(ctx, p.rc)
	if err != nil {
		return err
	}
	p.target = target
	if p.objs, err = build(ctx, p.rc, p.rc.Deps.Config.ManifestDir, ports.Ref(target)); err != nil {
		return err
	}
	findings := p.rc.Deps.Applier.Lint(p.objs)
	if len(findings) == 0 {
		return nil
	}
	f := ports.Failf(deploy.FailLintRefused, "manifest lint refused %d workload(s) at %s", len(findings), short(target))
	for _, l := range findings {
		f.LogTail = append(f.LogTail, fmt.Sprintf("%s %s/%s: %s", l.Object.Kind, l.Object.Namespace, l.Object.Name, l.Reason))
	}
	return f
}

// resolveTarget returns the run's target commit. A run started without one
// (reapply rolls main as it is) gets main's head, persisted so a resumed run
// reads the same commit instead of whatever main has become.
func resolveTarget(ctx context.Context, rc *stage.RunCtx) (deploy.SHA, error) {
	if sha := rc.View().TargetSHA; sha != "" {
		return sha, nil
	}
	sha, err := rc.Deps.GitHub.BranchHead(ctx, rc.Deps.Config.MainBranch)
	if err != nil {
		return "", ports.Failf(deploy.FailGitHub, "read %s head: %v", rc.Deps.Config.MainBranch, err)
	}
	return sha, rc.Update(ctx, func(r *deploy.Run) { r.TargetSHA = sha })
}

// checks waits for the target's required checks. Pending waits rather than
// fails: a train started right after a merge meets checks still running on
// main.
func (p *checkRun) checks(ctx context.Context) error {
	err := poll(ctx, p.rc, p.checksGreen)
	p.wait(ctx, "")
	return err
}

func (p *checkRun) checksGreen(ctx context.Context) (bool, error) {
	sum, err := p.rc.Deps.GitHub.Checks(ctx, p.target)
	if err != nil {
		return false, ports.Failf(deploy.FailGitHub, "read checks on %s: %v", short(p.target), err)
	}
	switch sum.State {
	case deploy.ChecksFailure:
		return false, p.checksFailed(ctx, sum)
	case deploy.ChecksPending:
		p.wait(ctx, fmt.Sprintf("waiting on required checks on %s", short(p.target)))
		return false, nil
	default:
		return true, nil
	}
}

// wait publishes msg once, not on every poll; "" returns the stage to
// running.
func (p *checkRun) wait(ctx context.Context, msg string) {
	if msg != p.waiting {
		p.waiting = msg
		p.rc.Waiting(ctx, msg)
	}
}

func (p *checkRun) checksFailed(ctx context.Context, sum ports.CheckSummary) error {
	f := ports.Failf(deploy.FailChecksFailed, "required checks failed on %s", short(p.target))
	for _, c := range sum.Checks {
		if p.blocking(c) {
			f.LogTail = append(f.LogTail, c.Evidence()...)
			p.rc.AddLink(ctx, deploy.Link{Label: c.Name, URL: c.URL})
		}
	}
	return f
}

// blocking is a failed check the summary state counts: a required one or
// CodeScene.
func (p *checkRun) blocking(c ports.Check) bool {
	if c.State != deploy.ChecksFailure {
		return false
	}
	return c.Required || c.Name == p.rc.Deps.Config.CodeSceneCheck
}

// settled refuses to start while a managed workload is mid-rollout or
// unhealthy: the train would wait on, and then be blamed for, a rollout it
// did not start.
func (p *checkRun) settled(ctx context.Context) error {
	refs := refsOf(managedUnits(arrange(p.objs).units))
	bad, err := p.rc.Deps.Watcher.Settled(ctx, refs)
	if err != nil {
		return ports.Failf(deploy.FailKube, "read workload status: %v", err)
	}
	if len(bad) == 0 {
		return nil
	}
	f := ports.Failf(deploy.FailNotSettled, "%d workload(s) are mid-rollout or unhealthy", len(bad))
	for _, u := range bad {
		f.LogTail = append(f.LogTail, fmt.Sprintf("%s %s/%s: %s", u.Workload.Kind, u.Workload.Namespace, u.Workload.Name, u.Reason))
	}
	return f
}
