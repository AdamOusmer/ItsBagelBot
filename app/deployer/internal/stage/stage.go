// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package stage

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type Stage interface {
	ID() deploy.StageID
	Done(ctx context.Context, rc *RunCtx) (bool, error)
	Run(ctx context.Context, rc *RunCtx) error
}

var (
	ErrSkipped   = errors.New("stage skipped")
	ErrCancelled = errors.New("run cancelled")
)

type Fail = ports.Fail

func Failf(code deploy.FailureCode, format string, args ...any) *Fail {
	return ports.Failf(code, format, args...)
}

type Config = ports.Config

type Deps struct {
	GitHub   ports.GitHub
	Registry ports.Registry
	Applier  ports.Applier
	Watcher  ports.Watcher
	Claims   ports.ClaimsPusher
	Clock    ports.Clock
	Config   ports.Config
	Log      *zap.Logger
}

type Sink interface {
	// Slices and maps inside may be shared: treat them as read-only.
	View() deploy.Run
	Update(ctx context.Context, edit func(*deploy.Run)) error
	AwaitApproval(ctx context.Context, id deploy.StageID, reason string) error
}

type RunCtx struct {
	Deps  Deps
	stage deploy.StageID
	sink  Sink
}

func New(id deploy.StageID, deps Deps, sink Sink) *RunCtx {
	return &RunCtx{Deps: deps, stage: id, sink: sink}
}

func (rc *RunCtx) StageID() deploy.StageID { return rc.stage }

func (rc *RunCtx) View() deploy.Run { return rc.sink.View() }

func (rc *RunCtx) Cancelled() bool { return rc.sink.View().CancelRequested }

// Update's error must be handled: a resumed run relies on Outputs.
func (rc *RunCtx) Update(ctx context.Context, edit func(*deploy.Run)) error {
	return rc.sink.Update(ctx, edit)
}

func (rc *RunCtx) SetOutputs(ctx context.Context, edit func(*deploy.Outputs)) error {
	return rc.sink.Update(ctx, func(r *deploy.Run) { edit(&r.Outputs) })
}

func (rc *RunCtx) SetProgress(ctx context.Context, p deploy.Progress) {
	rc.report(ctx, func(s *deploy.Stage) { s.Progress = p })
}

func (rc *RunCtx) SetItems(ctx context.Context, items []deploy.Item) {
	rc.report(ctx, func(s *deploy.Stage) { s.Items = append([]deploy.Item(nil), items...) })
}

func (rc *RunCtx) SetItem(ctx context.Context, item deploy.Item) {
	rc.report(ctx, func(s *deploy.Stage) { s.Items = upsertItem(s.Items, item) })
}

func (rc *RunCtx) AddLink(ctx context.Context, l deploy.Link) {
	rc.report(ctx, func(s *deploy.Stage) { s.Links = addLink(s.Links, l) })
}

func (rc *RunCtx) Waiting(ctx context.Context, msg string) {
	rc.report(ctx, func(s *deploy.Stage) { s.Waiting, s.State = msg, waitingState(msg) })
}

func (rc *RunCtx) AwaitApproval(ctx context.Context, reason string) error {
	return rc.sink.AwaitApproval(ctx, rc.stage, reason)
}

func (rc *RunCtx) report(ctx context.Context, edit func(*deploy.Stage)) {
	err := rc.sink.Update(ctx, func(r *deploy.Run) {
		if s := r.Stage(rc.stage); s != nil {
			edit(s)
		}
	})
	if err != nil {
		rc.Deps.Log.Warn("progress persist failed", zap.String("stage", string(rc.stage)), zap.Error(err))
	}
}

func waitingState(msg string) deploy.StageState {
	if msg == "" {
		return deploy.StateRunning
	}
	return deploy.StateWaiting
}

// Must copy: an earlier View may be marshalling the old backing array.
func upsertItem(items []deploy.Item, item deploy.Item) []deploy.Item {
	out := append(make([]deploy.Item, 0, len(items)+1), items...)
	for i := range out {
		if out[i].Key == item.Key {
			out[i] = item
			return out
		}
	}
	return append(out, item)
}

func addLink(links []deploy.Link, l deploy.Link) []deploy.Link {
	for _, have := range links {
		if have.URL == l.URL {
			return links
		}
	}
	return append(links, l)
}
