// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/bus"
)

const (
	keyIndex  kvKey = "index"
	keyActive kvKey = "active"
	keyLock   kvKey = "lock"
)

func runKey(id deploy.RunID) kvKey { return kvKey("run." + string(id)) }

type Store struct {
	kv    bucket
	ttl   time.Duration
	clock ports.Clock
}

var _ ports.Store = (*Store)(nil)

func New(ctx context.Context, js jetstream.JetStream, cfg ports.Config) (*Store, error) {
	kv, err := bus.CoordinationBucket(ctx, js, deploy.KVBucket, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s bucket: %w", deploy.KVBucket, err)
	}
	return newStore(jsBucket{kv: kv}, cfg.LockTTL, ports.SystemClock{}), nil
}

func newStore(kv bucket, lockTTL time.Duration, clock ports.Clock) *Store {
	return &Store{kv: kv, ttl: lockTTL, clock: clock}
}

func (s *Store) Get(ctx context.Context, id deploy.RunID) (deploy.Run, ports.Revision, error) {
	run, rev, found, err := load[deploy.Run](ctx, s.kv, runKey(id))
	if err == nil && !found {
		err = fmt.Errorf("%w: run %s", ports.ErrNotFound, id)
	}
	return run, rev, err
}

// Write order (active pointer, index, run) must hold: a partial failure then names only an unwritten run.
func (s *Store) Put(ctx context.Context, run *deploy.Run, rev ports.Revision) (ports.Revision, error) {
	if err := s.claimActive(ctx, run); err != nil {
		return 0, err
	}
	if err := s.index(ctx, run.ID, rev); err != nil {
		return 0, err
	}
	next, err := s.save(ctx, runKey(run.ID), run, rev)
	if err != nil {
		return 0, err
	}
	s.releaseActive(ctx, run)
	return next, nil
}

func (s *Store) Active(ctx context.Context) (deploy.Run, ports.Revision, bool, error) {
	id, _, found, err := load[deploy.RunID](ctx, s.kv, keyActive)
	if !found {
		return deploy.Run{}, 0, false, err
	}
	run, rev, found, err := load[deploy.Run](ctx, s.kv, runKey(id))
	if !found || run.State.Terminal() {
		return deploy.Run{}, 0, false, err
	}
	return run, rev, true, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]deploy.RunSummary, error) {
	ids, _, _, err := load[[]deploy.RunID](ctx, s.kv, keyIndex)
	if err != nil {
		return nil, err
	}
	ids = ids[:min(len(ids), clampLimit(limit))]
	out := make([]deploy.RunSummary, 0, len(ids))
	for _, id := range ids {
		run, _, found, err := load[deploy.Run](ctx, s.kv, runKey(id))
		if err != nil {
			return nil, err
		}
		if found {
			out = append(out, run.Summary())
		}
	}
	return out, nil
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > deploy.KeepRuns {
		return deploy.KeepRuns
	}
	return limit
}

func (s *Store) claimActive(ctx context.Context, run *deploy.Run) error {
	if run.State.Terminal() {
		return nil
	}
	holder, rev, found, err := load[deploy.RunID](ctx, s.kv, keyActive)
	if err != nil || holder == run.ID {
		return err
	}
	if err := s.refuseLiveHolder(ctx, holder, found); err != nil {
		return err
	}
	_, err = s.save(ctx, keyActive, run.ID, rev)
	return err
}

func (s *Store) refuseLiveHolder(ctx context.Context, holder deploy.RunID, found bool) error {
	if !found {
		return nil
	}
	cur, _, exists, err := load[deploy.Run](ctx, s.kv, runKey(holder))
	if err != nil {
		return err
	}
	if exists && !cur.State.Terminal() {
		return fmt.Errorf("%w: run %s is active", ports.ErrConflict, holder)
	}
	return nil
}

func (s *Store) releaseActive(ctx context.Context, run *deploy.Run) {
	if !run.State.Terminal() {
		return
	}
	holder, rev, found, _ := load[deploy.RunID](ctx, s.kv, keyActive)
	if found && holder == run.ID {
		_ = s.kv.remove(ctx, keyActive, rev)
	}
}

func (s *Store) index(ctx context.Context, id deploy.RunID, rev ports.Revision) error {
	if rev != 0 {
		return nil
	}
	ids, irev, _, err := load[[]deploy.RunID](ctx, s.kv, keyIndex)
	if err != nil || slices.Contains(ids, id) {
		return err
	}
	ids = slices.Insert(ids, 0, id)
	cut := min(len(ids), deploy.KeepRuns)
	if _, err := s.save(ctx, keyIndex, ids[:cut], irev); err != nil {
		return err
	}
	return s.prune(ctx, ids[cut:])
}

func (s *Store) prune(ctx context.Context, ids []deploy.RunID) error {
	var errs []error
	for _, id := range ids {
		errs = append(errs, s.kv.remove(ctx, runKey(id), 0))
	}
	return errors.Join(errs...)
}
