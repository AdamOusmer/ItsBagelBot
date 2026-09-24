// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type lockRecord struct {
	Owner       deploy.RunID `json:"owner"`
	HeartbeatAt time.Time    `json:"heartbeat_at"`
}

func (s *Store) AcquireLock(ctx context.Context, owner deploy.RunID) (ports.Lock, error) {
	cur, rev, found, err := load[lockRecord](ctx, s.kv, keyLock)
	if err != nil {
		return ports.Lock{}, err
	}
	if found && !s.claimable(cur, owner) {
		return ports.Lock{}, fmt.Errorf("%w: held by run %s", ports.ErrLockHeld, cur.Owner)
	}
	return s.stamp(ctx, owner, rev)
}

// The zero Lock must be refused: revision 0 would create an ownerless lock.
func (s *Store) Heartbeat(ctx context.Context, lock ports.Lock) (ports.Lock, error) {
	if lock.Revision == 0 {
		return ports.Lock{}, fmt.Errorf("%w: lock not held", ports.ErrLockHeld)
	}
	return s.stamp(ctx, lock.Owner, lock.Revision)
}

// The zero Lock must be a no-op: a revision-0 delete is unconditional and drops another run's lock.
func (s *Store) ReleaseLock(ctx context.Context, lock ports.Lock) error {
	if lock.Revision == 0 {
		return nil
	}
	err := s.kv.remove(ctx, keyLock, lock.Revision)
	if errors.Is(err, ports.ErrConflict) {
		return nil
	}
	return err
}

func (s *Store) LockOwner(ctx context.Context) (deploy.RunID, bool, error) {
	cur, _, found, err := load[lockRecord](ctx, s.kv, keyLock)
	if !found || s.expired(cur) {
		return "", false, err
	}
	return cur.Owner, true, nil
}

func (s *Store) claimable(cur lockRecord, owner deploy.RunID) bool {
	return cur.Owner == owner || s.expired(cur)
}

func (s *Store) expired(cur lockRecord) bool {
	return !s.clock.Now().Before(cur.HeartbeatAt.Add(s.ttl))
}

func (s *Store) stamp(ctx context.Context, owner deploy.RunID, rev ports.Revision) (ports.Lock, error) {
	next, err := s.save(ctx, keyLock, lockRecord{Owner: owner, HeartbeatAt: s.clock.Now()}, rev)
	if errors.Is(err, ports.ErrConflict) {
		return ports.Lock{}, fmt.Errorf("%w: %w", ports.ErrLockHeld, err)
	}
	if err != nil {
		return ports.Lock{}, err
	}
	return ports.Lock{Owner: owner, Revision: next}, nil
}
