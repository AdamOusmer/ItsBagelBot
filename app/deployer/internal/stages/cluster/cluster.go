// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package cluster holds the cluster-side stages: preflight, acl, rollout and
// verify, plus the rollout order.
package cluster

import (
	"context"
	"errors"
	"path"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// All returns this package's stages in train order.
func All() []stage.Stage {
	return []stage.Stage{preflight{}, acl{}, rollout{}, verify{}}
}

// pinRef is the commit the cluster stages read manifests at: the pin PR's
// merge commit, or the target commit for a reapply, which has no pin PR and
// rolls the pins already on main.
func pinRef(run deploy.Run) ports.Ref {
	if run.Outputs.PinSHA != "" {
		return ports.Ref(run.Outputs.PinSHA)
	}
	return ports.Ref(run.TargetSHA)
}

// build reads dir at ref through the GitHub API and builds it the way
// kubectl apply -k would. Never the working tree: the cluster must get
// exactly the merged commit, not whatever the deployer image was built from.
func build(ctx context.Context, rc *stage.RunCtx, dir ports.FilePath, ref ports.Ref) (ports.Objects, error) {
	files, err := rc.Deps.GitHub.Tree(ctx, dir, ref)
	if err != nil {
		return nil, ports.Failf(deploy.FailGitHub, "read %s at %s: %v", dir, ref, err)
	}
	return rc.Deps.Applier.Build(ctx, ports.BuildSpec{Files: files, Root: dir})
}

// statusRoutes builds the db status routes file on its own. Its directory
// has no kustomization.yaml and holds other manifests (certificates, db
// network policies) the train does not own, so only this one file goes in.
func statusRoutes(ctx context.Context, rc *stage.RunCtx, ref ports.Ref) (ports.Objects, error) {
	file := rc.Deps.Config.StatusRoutes
	body, err := rc.Deps.GitHub.File(ctx, file, ref)
	if err != nil {
		return nil, ports.Failf(deploy.FailGitHub, "read %s at %s: %v", file, ref, err)
	}
	root := ports.FilePath(path.Dir(string(file)))
	return rc.Deps.Applier.Build(ctx, ports.BuildSpec{Files: ports.Files{file: body}, Root: root})
}

// poll runs check every PollEvery until it reports done or fails, ctx ends,
// or cancel is requested. Only waits on things outside the deployer (checks,
// NATS reloads) poll, so stopping one for a cancel leaves nothing half done.
func poll(ctx context.Context, rc *stage.RunCtx, check func(context.Context) (bool, error)) error {
	tick := time.NewTicker(rc.Deps.Config.PollEvery)
	defer tick.Stop()
	for {
		done, err := check(ctx)
		if done || err != nil {
			return err
		}
		if rc.Cancelled() {
			return stage.ErrCancelled
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// pause waits d or until ctx ends.
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// timedOut reports whether err is the deadline of a wait bounded inside the
// stage. The parent ending (shutdown, engine cancel) is not a timeout.
func timedOut(parent context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil
}

// applyErr keeps an applier Fail (an allowlist refusal) as is and files any
// other apply error as a failed apply of what.
func applyErr(what string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ports.AsFail(err); ok {
		return err
	}
	return ports.Failf(deploy.FailApplyFailed, "apply %s: %v", what, err)
}

// stateOf is the item state a step's result maps to.
func stateOf(err error) deploy.StageState {
	switch {
	case err == nil:
		return deploy.StateSucceeded
	case errors.Is(err, stage.ErrSkipped):
		return deploy.StateSkipped
	case errors.Is(err, stage.ErrCancelled):
		return deploy.StateCancelled
	default:
		return deploy.StateFailed
	}
}

// detailOf is the one-line reason an item shows for err.
func detailOf(err error) string {
	if err == nil {
		return ""
	}
	if f, ok := ports.AsFail(err); ok {
		return f.Message
	}
	return err.Error()
}

// short is a sha as the page shows it.
func short(sha deploy.SHA) string {
	if len(sha) > 12 {
		return string(sha[:12])
	}
	return string(sha)
}
