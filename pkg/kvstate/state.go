// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kvstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type Store interface {
	Get(context.Context, string) (jetstream.KeyValueEntry, error)
	Create(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error)
	Update(context.Context, string, []byte, uint64) (uint64, error)
}

type Value struct {
	Data     []byte
	Revision uint64
	Created  time.Time
}

func Key(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func Read(ctx context.Context, store Store, key string) (Value, error) {
	entry, err := store.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return Value{}, nil
	}
	if err != nil {
		return Value{}, err
	}
	return Value{entry.Value(), entry.Revision(), entry.Created()}, nil
}

// edit may run more than once; a timeout is ambiguous and must never authorize a side effect.
func Change(ctx context.Context, store Store, key string, edit func(Value) ([]byte, error)) (uint64, error) {
	for range 16 {
		old, err := Read(ctx, store, key)
		if err != nil {
			return 0, err
		}
		data, err := edit(old)
		if err != nil {
			return 0, err
		}
		rev, err := write(ctx, store, key, Value{Data: data, Revision: old.Revision})
		if !lostRace(err) {
			return rev, err
		}
	}
	return 0, errors.New("coordination contention: retry delivery")
}

func lostRace(err error) bool {
	return errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyRevisionMismatch)
}

func write(ctx context.Context, store Store, key string, value Value) (uint64, error) {
	if value.Revision == 0 {
		return store.Create(ctx, key, value.Data)
	}
	return store.Update(ctx, key, value.Data, value.Revision)
}
