// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func preflightDone(context.Context, *stage.RunCtx) (bool, error) { return false, nil }

func runPreflight(ctx context.Context, rc *stage.RunCtx) error {
	target, err := resolveTarget(ctx, rc)
	if err != nil {
		return err
	}
	waiting := ""
	err = poll(ctx, rc, func(ctx context.Context) (bool, error) {
		green, msg, err := checksGreen(ctx, rc, target)
		if msg != waiting {
			waiting = msg
			rc.Waiting(ctx, msg)
		}
		return green, err
	})
	return err
}

func resolveTarget(ctx context.Context, rc *stage.RunCtx) (deploy.SHA, error) {
	if sha := rc.View().TargetSHA; sha != "" {
		return sha, nil
	}
	sha, err := rc.Deps.GitHub.BranchHead(ctx, rc.Deps.Config.MainBranch)
	if err != nil {
		return "", fail(deploy.FailGitHub, "read %s head: %v", rc.Deps.Config.MainBranch, err)
	}
	return sha, rc.Update(ctx, func(r *deploy.Run) { r.TargetSHA = sha })
}

func checksGreen(ctx context.Context, rc *stage.RunCtx, target deploy.SHA) (bool, string, error) {
	sum, err := rc.Deps.GitHub.Checks(ctx, target)
	if err != nil {
		return false, "", fail(deploy.FailGitHub, "read checks on %s: %v", short(target), err)
	}
	switch sum.State {
	case deploy.ChecksFailure:
		return false, "", mainChecksFailed(ctx, rc, target, sum)
	case deploy.ChecksPending:
		return false, fmt.Sprintf("waiting on required checks on %s", short(target)), nil
	}
	return true, "", nil
}

func mainChecksFailed(ctx context.Context, rc *stage.RunCtx, target deploy.SHA, sum ports.CheckSummary) error {
	f := fail(deploy.FailChecksFailed, "required checks failed on %s", short(target))
	for _, c := range sum.Checks {
		if blocking(c, rc.Deps.Config.CodeSceneCheck) {
			f.LogTail = append(f.LogTail, c.Evidence()...)
			rc.AddLink(ctx, deploy.Link{Label: c.Name, URL: c.URL})
		}
	}
	return f
}

func blocking(c ports.Check, codescene string) bool {
	if c.State != deploy.ChecksFailure {
		return false
	}
	return c.Required || c.Name == codescene
}

func promoteDone(ctx context.Context, rc *stage.RunCtx) (bool, error) {
	sha, err := promotionSHA(ctx, rc)
	if err != nil {
		return false, err
	}
	head, err := rc.Deps.GitHub.BranchHead(ctx, rc.Deps.Config.DeployBranch)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	return head == sha, err
}

func runPromote(ctx context.Context, rc *stage.RunCtx) error {
	sha, err := promotionSHA(ctx, rc)
	if err != nil {
		return err
	}
	return rc.Deps.GitHub.MoveBranch(ctx, rc.Deps.Config.DeployBranch, sha)
}

func promotionSHA(ctx context.Context, rc *stage.RunCtx) (deploy.SHA, error) {
	sha := promoteTarget(rc.View())
	if sha == "" {
		return "", fail(deploy.FailInternal, "no pin or target commit to promote")
	}
	onMain, err := OnMain(ctx, rc.Deps.GitHub, rc.Deps.Config.MainBranch, sha)
	if err != nil {
		return "", err
	}
	if !onMain {
		return "", fail(deploy.FailGitHub, "refusing to promote %s: it is not on %s", short(sha), rc.Deps.Config.MainBranch)
	}
	return sha, nil
}

func promoteTarget(run deploy.Run) deploy.SHA {
	if run.Outputs.PinSHA != "" {
		return run.Outputs.PinSHA
	}
	return run.TargetSHA
}
