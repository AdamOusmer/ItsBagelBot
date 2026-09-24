// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kvtest

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type Store struct {
	mu       sync.Mutex
	entries  map[string]entry
	revision uint64
	Err      error
	Now      time.Time
}

type entry struct {
	jetstream.KeyValueEntry
	data     []byte
	revision uint64
	created  time.Time
}

func (e entry) Value() []byte      { return append([]byte(nil), e.data...) }
func (e entry) Revision() uint64   { return e.revision }
func (e entry) Created() time.Time { return e.created }

func New() *Store { return &Store{entries: map[string]entry{}, Now: time.Unix(100, 0)} }

func (s *Store) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure(ctx); err != nil {
		return nil, err
	}
	value, ok := s.entries[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return value, nil
}

func (s *Store) Create(ctx context.Context, key string, data []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	return s.Update(ctx, key, data, 0)
}

func (s *Store) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.failure(ctx); err != nil {
		return 0, err
	}
	if s.entries[key].revision != revision {
		return 0, jetstream.ErrKeyExists
	}
	s.revision++
	s.entries[key] = entry{data: append([]byte(nil), data...), revision: s.revision, created: s.Now}
	return s.revision, nil
}

func (s *Store) failure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Err
}
