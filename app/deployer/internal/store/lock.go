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

// lockRecord is the lock document. Expiry is HeartbeatAt plus LockTTL, judged
// on the reader's clock.
//
// A KV per-key TTL (KeyTTL, server 2.11+) looks like the natural fit but
// cannot carry a heartbeat: the TTL is set only by Create, Update cannot
// extend it, so every heartbeat would be delete-then-create, which drops the
// revision CAS and leaves a window where a second run takes the lock between
// the two writes. The explicit timestamp keeps every write a CAS.
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

// Heartbeat rewrites the timestamp at the held revision. It still succeeds
// after LockTTL if nobody took the lock in the meantime: the revision proves
// the lock was never anyone else's. The zero Lock is refused rather than
// stamped at revision 0, which would create an ownerless lock nobody can
// re-acquire until it expires.
func (s *Store) Heartbeat(ctx context.Context, lock ports.Lock) (ports.Lock, error) {
	if lock.Revision == 0 {
		return ports.Lock{}, fmt.Errorf("%w: lock not held", ports.ErrLockHeld)
	}
	return s.stamp(ctx, lock.Owner, lock.Revision)
}

// ReleaseLock deletes the lock at the held revision. A lock another run has
// taken over is not ours to delete, so a lost CAS is not an error. The zero
// Lock is a no-op rather than a revision-0 delete, which KV treats as
// unconditional: a caller that assigns a failed Heartbeat's zero result and
// then releases would otherwise delete the lock the other run now holds.
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

// stamp writes owner's heartbeat at rev (0 creates). A lost CAS means
// another run wrote the lock after it was read, so it reads as ErrLockHeld.
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
