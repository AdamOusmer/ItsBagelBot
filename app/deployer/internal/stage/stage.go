// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package stage is the contract between the run engine and the stages of the
// release train. A stage is idempotent: Done reports whether its effect
// already exists, so a resumed run skips finished work instead of redoing it
// (a second tag push, a second merge), and Run is safe to call again after a
// failure part way through.
//
// Stages never touch the Run directly. Every read goes through View and every
// write through Update, which the engine serialises against the rpc verbs
// (cancel and approve arrive on other goroutines) and follows with a KV put
// and an event publish, so the page and the store always see the same
// snapshot.
package stage

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// Stage is one step of the train.
type Stage interface {
	ID() deploy.StageID
	// Done reports whether the stage's effect already exists. The engine
	// calls it before Run on every attempt; true marks the stage succeeded
	// without running it.
	Done(ctx context.Context, rc *RunCtx) (bool, error)
	// Run does the work. It returns nil on success, ErrSkipped when the stage
	// does not apply to this run (no PRs, messaging unchanged), ErrCancelled
	// when it stopped at a safe point because cancel was requested, a *Fail
	// for an operator-meaningful failure, or any other error (recorded as
	// deploy.FailInternal).
	Run(ctx context.Context, rc *RunCtx) error
}

var (
	// ErrSkipped marks the stage skipped.
	ErrSkipped = errors.New("stage skipped")
	// ErrCancelled ends the run as cancelled.
	ErrCancelled = errors.New("run cancelled")
)

// Fail is the operator-meaningful failure type; see ports.Fail.
type Fail = ports.Fail

// Failf builds a Fail; see ports.Failf.
func Failf(code deploy.FailureCode, format string, args ...any) *Fail {
	return ports.Failf(code, format, args...)
}

// Config is the deployer's tuning; see ports.Config.
type Config = ports.Config

// Deps are the ports a stage may use.
type Deps struct {
	GitHub   ports.GitHub
	Registry ports.Registry
	Applier  ports.Applier
	Watcher  ports.Watcher
	Clock    ports.Clock
	Config   ports.Config
	Log      *zap.Logger
}

// Sink is the engine side of a RunCtx: it owns the run's lock, persistence
// and publishing.
type Sink interface {
	// View returns a copy of the run taken under the run's lock. Slices and
	// maps inside may be shared: treat them as read-only.
	View() deploy.Run
	// Update applies edit under the run's lock, bumps Seq and UpdatedAt,
	// persists the run and publishes the snapshot. A persist failure is
	// returned after the in-memory edit has been applied.
	Update(ctx context.Context, edit func(*deploy.Run)) error
	// AwaitApproval marks the stage waiting with NeedsApproval = reason and
	// blocks until the approve verb names this stage (nil), cancel is
	// requested (ErrCancelled), or ctx ends.
	AwaitApproval(ctx context.Context, id deploy.StageID, reason string) error
}

// RunCtx is what a stage sees of the run it belongs to.
type RunCtx struct {
	Deps  Deps
	stage deploy.StageID
	sink  Sink
}

// New binds a RunCtx to one stage of one run. Called by the engine.
func New(id deploy.StageID, deps Deps, sink Sink) *RunCtx {
	return &RunCtx{Deps: deps, stage: id, sink: sink}
}

// StageID is the stage this context reports for.
func (rc *RunCtx) StageID() deploy.StageID { return rc.stage }

// View returns a read-only copy of the run.
func (rc *RunCtx) View() deploy.Run { return rc.sink.View() }

// Cancelled reports whether cancel was requested. Stages check it at their
// safe points (rollout: between services) and return ErrCancelled.
func (rc *RunCtx) Cancelled() bool { return rc.sink.View().CancelRequested }

// Update edits the whole run. Use it for Outputs; a resumed run relies on
// them, so the error must be handled.
func (rc *RunCtx) Update(ctx context.Context, edit func(*deploy.Run)) error {
	return rc.sink.Update(ctx, edit)
}

// SetOutputs edits the run's Outputs.
func (rc *RunCtx) SetOutputs(ctx context.Context, edit func(*deploy.Outputs)) error {
	return rc.sink.Update(ctx, func(r *deploy.Run) { edit(&r.Outputs) })
}

// The reporters below are cosmetic progress: a failed persist is logged and
// dropped, because the next Update writes the whole run again and a stage
// must not fail over a progress bar.

// SetProgress sets the stage's progress bar.
func (rc *RunCtx) SetProgress(ctx context.Context, p deploy.Progress) {
	rc.report(ctx, func(s *deploy.Stage) { s.Progress = p })
}

// SetItems replaces the stage's rows, e.g. to seed every service as pending.
func (rc *RunCtx) SetItems(ctx context.Context, items []deploy.Item) {
	rc.report(ctx, func(s *deploy.Stage) { s.Items = append([]deploy.Item(nil), items...) })
}

// SetItem inserts or replaces the row with item.Key.
func (rc *RunCtx) SetItem(ctx context.Context, item deploy.Item) {
	rc.report(ctx, func(s *deploy.Stage) { s.Items = upsertItem(s.Items, item) })
}

// AddLink appends a link unless one with the same URL exists.
func (rc *RunCtx) AddLink(ctx context.Context, l deploy.Link) {
	rc.report(ctx, func(s *deploy.Stage) { s.Links = addLink(s.Links, l) })
}

// Waiting marks the stage waiting on something outside the deployer; an
// empty msg returns it to running.
func (rc *RunCtx) Waiting(ctx context.Context, msg string) {
	rc.report(ctx, func(s *deploy.Stage) { s.Waiting, s.State = msg, waitingState(msg) })
}

// AwaitApproval pauses the stage until the approve verb; see Sink.
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

// upsertItem copies before replacing: a View taken earlier shares the old
// backing array and may be marshalling it on an rpc goroutine right now.
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
