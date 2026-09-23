// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package store keeps runs and the cluster-wide deploy lock in the
// DEPLOY_RUNS JetStream KV bucket on the hub.
//
// Keys: run.<id> holds one run; index is the newest-first id list List reads;
// active names the non-terminal run; lock is the deploy lock. Every write is
// a revision compare-and-set, so two writers can never both win.
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

// Store implements ports.Store.
type Store struct {
	kv    bucket
	ttl   time.Duration
	clock ports.Clock
}

var _ ports.Store = (*Store)(nil)

// New opens (creating if needed) the bucket. It is a CoordinationBucket (R3,
// file storage, history 1) like the outgress buckets on the same hub, with no
// TTL: runs leave by the KeepRuns prune, not by age, so a run left failed over
// a long weekend is still there to resume.
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

// Put claims the active pointer, indexes a new run, then writes the run. In
// that order a failure between any two steps leaves a pointer or index entry
// naming a run that was never written, which every read treats as absent,
// instead of a written run that List or Active cannot see.
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

// List reads the runs one Get at a time. Keeping summaries in the index would
// make it one read, but then every state change would rewrite the index as
// well as the run, and the rollout stage writes one per pod transition.
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

// clampLimit maps 0 (the wire default) and anything past the retained history
// to KeepRuns.
func clampLimit(limit int) int {
	if limit <= 0 || limit > deploy.KeepRuns {
		return deploy.KeepRuns
	}
	return limit
}

// claimActive points the active key at a non-terminal run. It refuses with
// ErrConflict while another non-terminal run holds the pointer: the engine
// checks Active before Start, and this CAS is what makes two concurrent
// starts unable to both pass that check. A pointer left at a terminal or
// never-written run is taken over.
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

// releaseActive clears the pointer once its run is terminal. Best effort:
// Active and claimActive both read a pointer at a terminal run as empty, so a
// failed clear costs one extra Get on the next read, while failing the Put for
// it would report a run write that landed as lost.
func (s *Store) releaseActive(ctx context.Context, run *deploy.Run) {
	if !run.State.Terminal() {
		return
	}
	holder, rev, found, _ := load[deploy.RunID](ctx, s.kv, keyActive)
	if found && holder == run.ID {
		_ = s.kv.remove(ctx, keyActive, rev)
	}
}

// index puts a new run (rev 0) at the head of the index and deletes the runs
// pushed past KeepRuns. It is idempotent so a create retried after a failed
// run write does not list the run twice.
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
