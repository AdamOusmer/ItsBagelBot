// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

func All() []stage.Stage {
	return []stage.Stage{preflight{}, acl{}, rollout{}, verify{}}
}

func pinRef(run deploy.Run) ports.Ref {
	if run.Outputs.PinSHA != "" {
		return ports.Ref(run.Outputs.PinSHA)
	}
	return ports.Ref(run.TargetSHA)
}

func build(ctx context.Context, rc *stage.RunCtx, dir ports.FilePath, ref ports.Ref) (ports.Objects, error) {
	files, err := rc.Deps.GitHub.Tree(ctx, dir, ref)
	if err != nil {
		return nil, ports.Failf(deploy.FailGitHub, "read %s at %s: %v", dir, ref, err)
	}
	return rc.Deps.Applier.Build(ctx, ports.BuildSpec{Files: files, Root: dir})
}

func statusRoutes(ctx context.Context, rc *stage.RunCtx, ref ports.Ref) (ports.Objects, error) {
	file := rc.Deps.Config.StatusRoutes
	body, err := rc.Deps.GitHub.File(ctx, file, ref)
	if err != nil {
		return nil, ports.Failf(deploy.FailGitHub, "read %s at %s: %v", file, ref, err)
	}
	root := ports.FilePath(path.Dir(string(file)))
	return rc.Deps.Applier.Build(ctx, ports.BuildSpec{Files: ports.Files{file: body}, Root: root})
}

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

func timedOut(parent context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil
}

func applyErr(what string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := ports.AsFail(err); ok {
		return err
	}
	return ports.Failf(deploy.FailApplyFailed, "apply %s: %v", what, err)
}

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

func detailOf(err error) string {
	if err == nil {
		return ""
	}
	if f, ok := ports.AsFail(err); ok {
		return f.Message
	}
	return err.Error()
}

func short(sha deploy.SHA) string {
	if len(sha) > 12 {
		return string(sha[:12])
	}
	return string(sha)
}
