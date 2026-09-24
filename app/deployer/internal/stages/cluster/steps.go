// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"errors"

	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type step struct {
	key   string
	label string
	run   func(context.Context) error
	last  *deploy.Item
}

func (s step) row(state deploy.StageState, detail string) deploy.Item {
	var it deploy.Item
	if s.last != nil {
		it = *s.last
	}
	it.Key, it.Label, it.State, it.Detail = s.key, s.label, state, detail
	return it
}

func (s step) result(err error) deploy.Item { return s.row(stateOf(err), detailOf(err)) }

type skip string

func (s skip) Error() string { return string(s) }

func (skip) Is(target error) bool { return target == stage.ErrSkipped }

func runSteps(ctx context.Context, rc *stage.RunCtx, steps []step) error {
	rows := make([]deploy.Item, len(steps))
	for i, s := range steps {
		rows[i] = s.row(deploy.StatePending, "")
	}
	rc.SetItems(ctx, rows)
	for i, s := range steps {
		if err := runStep(ctx, rc, s); err != nil {
			return err
		}
		rc.SetProgress(ctx, deploy.Progress{Done: i + 1, Total: len(steps)})
	}
	return nil
}

func runStep(ctx context.Context, rc *stage.RunCtx, s step) error {
	if rc.Cancelled() {
		rc.SetItem(ctx, s.row(deploy.StateCancelled, ""))
		return stage.ErrCancelled
	}
	rc.SetItem(ctx, s.row(deploy.StateRunning, ""))
	err := s.run(ctx)
	rc.SetItem(ctx, s.result(err))
	if errors.Is(err, stage.ErrSkipped) {
		return nil
	}
	return err
}
