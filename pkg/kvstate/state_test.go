// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kvstate_test

import (
	"context"
	"testing"

	"ItsBagelBot/pkg/kvstate"
	"ItsBagelBot/pkg/kvstate/kvtest"
)

type racingStore struct {
	*kvtest.Store
	races int
}

func (s *racingStore) Update(ctx context.Context, key string, data []byte, revision uint64) (uint64, error) {
	if s.races > 0 {
		s.races--
		if _, err := s.Store.Update(ctx, key, []byte("other"), revision); err != nil {
			return 0, err
		}
	}
	return s.Store.Update(ctx, key, data, revision)
}

func TestChangeRetriesALostUpdateRace(t *testing.T) {
	ctx := context.Background()
	store := &racingStore{Store: kvtest.New(), races: 2}
	if _, err := store.Create(ctx, "k", []byte("start")); err != nil {
		t.Fatal(err)
	}
	edits := 0
	if _, err := kvstate.Change(ctx, store, "k", func(kvstate.Value) ([]byte, error) {
		edits++
		return []byte("mine"), nil
	}); err != nil {
		t.Fatalf("a lost update race must retry, got %v", err)
	}
	if edits != 3 {
		t.Fatalf("want 3 edits (two lost races), got %d", edits)
	}
	got, err := kvstate.Read(ctx, store, "k")
	if err != nil || string(got.Data) != "mine" {
		t.Fatalf("final value %q, %v", got.Data, err)
	}
}

func TestChangeRetriesALostCreateRace(t *testing.T) {
	ctx := context.Background()
	store := kvtest.New()
	first := true
	if _, err := kvstate.Change(ctx, store, "k", func(kvstate.Value) ([]byte, error) {
		if first {
			first = false
			if _, err := store.Create(ctx, "k", []byte("other")); err != nil {
				t.Fatal(err)
			}
		}
		return []byte("mine"), nil
	}); err != nil {
		t.Fatalf("a lost create race must retry, got %v", err)
	}
}
