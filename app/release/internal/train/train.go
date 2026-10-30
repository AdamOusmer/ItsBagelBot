// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package train

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"ItsBagelBot/app/release/internal/ports"
	"ItsBagelBot/app/release/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

type File struct {
	mu   sync.Mutex
	path string
	run  deploy.Run
}

func Open(path string, seed deploy.Run) (*File, error) {
	f := &File{path: path, run: seed}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		f.run.Stages = pending(seed.Kind)
		return f, f.save()
	case err != nil:
		return nil, err
	}
	if err := codec.Unmarshal(b, &f.run); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return f, nil
}

func pending(kind deploy.RunKind) []deploy.Stage {
	ids := deploy.StagesFor(kind)
	out := make([]deploy.Stage, len(ids))
	for i, id := range ids {
		out[i] = deploy.Stage{ID: id, State: deploy.StatePending}
	}
	return out
}

func (f *File) View() deploy.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.run
}

func (f *File) Update(_ context.Context, edit func(*deploy.Run)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(&f.run)
	f.run.UpdatedAt = time.Now().UTC()
	return f.save()
}

func (f *File) save() error {
	b, err := codec.Marshal(&f.run)
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, b, 0o600)
}

type Train struct {
	deps stage.Deps
	byID map[deploy.StageID]stage.Stage
}

func New(deps stage.Deps, stages []stage.Stage) Train {
	byID := make(map[deploy.StageID]stage.Stage, len(stages))
	for _, s := range stages {
		byID[s.ID()] = s
	}
	return Train{deps: deps, byID: byID}
}

// An empty only drives every unfinished stage; the workflow passes one stage per job.
func (t Train) Drive(ctx context.Context, f *File, only deploy.StageID) error {
	for _, s := range f.View().Stages {
		if !due(s, only) {
			continue
		}
		if err := t.run(ctx, f, s.ID); err != nil {
			return err
		}
	}
	return finish(ctx, f)
}

func due(s deploy.Stage, only deploy.StageID) bool {
	if s.State == deploy.StateSucceeded || s.State == deploy.StateSkipped {
		return false
	}
	return only == "" || s.ID == only
}

func (t Train) run(ctx context.Context, f *File, id deploy.StageID) error {
	impl, ok := t.byID[id]
	if !ok {
		return fmt.Errorf("no implementation for stage %s", id)
	}
	return step(ctx, t.deps, f, impl)
}

func finish(ctx context.Context, f *File) error {
	if run := f.View(); run.CurrentStage() != "" {
		return nil
	}
	return f.Update(ctx, func(r *deploy.Run) { r.State = deploy.RunSucceeded })
}

func step(ctx context.Context, deps stage.Deps, f *File, s stage.Stage) error {
	id := s.ID()
	if err := f.Update(ctx, func(r *deploy.Run) { begin(r, id) }); err != nil {
		return err
	}
	rc := stage.New(id, deps, f)
	done, err := s.Done(ctx, rc)
	if err == nil && !done {
		err = s.Run(ctx, rc)
	}
	if saveErr := f.Update(ctx, func(r *deploy.Run) { end(r, id, err) }); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	if err != nil && !errors.Is(err, stage.ErrSkipped) {
		return fmt.Errorf("stage %s: %w", id, err)
	}
	return nil
}

func begin(r *deploy.Run, id deploy.StageID) {
	s, now := r.Stage(id), time.Now().UTC()
	s.State, s.StartedAt, s.EndedAt, s.Failure, s.Waiting = deploy.StateRunning, &now, nil, nil, ""
	s.Attempts++
	r.State, r.Failure = deploy.RunRunning, nil
}

func end(r *deploy.Run, id deploy.StageID, err error) {
	s, now := r.Stage(id), time.Now().UTC()
	s.EndedAt, s.Waiting = &now, ""
	switch {
	case err == nil:
		s.State = deploy.StateSucceeded
	case errors.Is(err, stage.ErrSkipped):
		s.State = deploy.StateSkipped
	default:
		failure := failureOf(err)
		s.State, s.Failure, r.Failure, r.State = deploy.StateFailed, &failure, &failure, deploy.RunFailed
	}
}

func failureOf(err error) deploy.Failure {
	if f, ok := ports.AsFail(err); ok {
		return f.Failure
	}
	return deploy.Failure{Code: deploy.FailInternal, Message: err.Error()}
}
