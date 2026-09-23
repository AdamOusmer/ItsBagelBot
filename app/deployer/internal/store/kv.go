// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/pkg/codec"
)

// kvKey is a key in the DEPLOY_RUNS bucket.
type kvKey string

// record is one value at its revision.
type record struct {
	value []byte
	rev   ports.Revision
}

// bucket is the slice of jetstream.KeyValue the store uses, with errors
// already folded onto the ports sentinels: get returns ErrNotFound for an
// absent or deleted key, and create, update and remove return ErrConflict when
// their revision precondition fails (remove at rev 0 is unconditional).
//
// It is narrowed so the CAS and lock-expiry rules are tested against an
// in-memory fake. go.mod carries no embedded nats-server, and the one KV
// integration test in the repo (outgress coordination) skips unless someone
// points it at a hand-run three-node fixture, so it never runs in CI.
type bucket interface {
	get(ctx context.Context, key kvKey) (record, error)
	create(ctx context.Context, key kvKey, value []byte) (ports.Revision, error)
	update(ctx context.Context, key kvKey, value []byte, rev ports.Revision) (ports.Revision, error)
	remove(ctx context.Context, key kvKey, rev ports.Revision) error
}

// jsBucket adapts a jetstream.KeyValue.
type jsBucket struct {
	kv jetstream.KeyValue
}

func (b jsBucket) get(ctx context.Context, key kvKey) (record, error) {
	e, err := b.kv.Get(ctx, string(key))
	if err != nil {
		return record{}, kvErr(err)
	}
	return record{value: e.Value(), rev: ports.Revision(e.Revision())}, nil
}

func (b jsBucket) create(ctx context.Context, key kvKey, value []byte) (ports.Revision, error) {
	rev, err := b.kv.Create(ctx, string(key), value)
	return ports.Revision(rev), kvErr(err)
}

func (b jsBucket) update(ctx context.Context, key kvKey, value []byte, rev ports.Revision) (ports.Revision, error) {
	next, err := b.kv.Update(ctx, string(key), value, uint64(rev))
	return ports.Revision(next), kvErr(err)
}

func (b jsBucket) remove(ctx context.Context, key kvKey, rev ports.Revision) error {
	return kvErr(b.kv.Delete(ctx, string(key), jetstream.LastRevision(uint64(rev))))
}

// kvErr maps the jetstream KV errors the store branches on. Both ErrKeyExists
// and ErrKeyRevisionMismatch are conflicts: on the R3 hub bucket a lost CAS
// arrives as API code 10164, which nats.go wraps as ErrKeyRevisionMismatch
// from Update/Delete but as ErrKeyExists from Create. Matching only one would
// surface a lost race in the other path as an opaque error instead of
// ErrConflict.
func kvErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, jetstream.ErrKeyNotFound):
		return fmt.Errorf("%w: %w", ports.ErrNotFound, err)
	case errors.Is(err, jetstream.ErrKeyExists), errors.Is(err, jetstream.ErrKeyRevisionMismatch):
		return fmt.Errorf("%w: %w", ports.ErrConflict, err)
	case errors.Is(err, jetstream.ErrInvalidKey):
		return fmt.Errorf("%w: %w", ports.ErrInvalid, err)
	}
	return err
}

// load reads key into a T. found is false, with a nil error, for an absent
// or deleted key.
func load[T any](ctx context.Context, kv bucket, key kvKey) (T, ports.Revision, bool, error) {
	var v T
	rec, err := kv.get(ctx, key)
	if errors.Is(err, ports.ErrNotFound) {
		return v, 0, false, nil
	}
	if err != nil {
		return v, 0, false, err
	}
	if err := codec.Unmarshal(rec.value, &v); err != nil {
		return v, 0, false, fmt.Errorf("decode %s: %w", key, err)
	}
	return v, rec.rev, true, nil
}

// save writes v at rev: 0 creates, anything else is a compare-and-set.
func (s *Store) save(ctx context.Context, key kvKey, v any, rev ports.Revision) (ports.Revision, error) {
	b, err := codec.Marshal(v)
	if err != nil {
		return 0, fmt.Errorf("encode %s: %w", key, err)
	}
	if rev == 0 {
		return s.kv.create(ctx, key, b)
	}
	return s.kv.update(ctx, key, b, rev)
}
