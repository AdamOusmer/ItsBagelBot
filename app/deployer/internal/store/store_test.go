// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func TestPutCompareAndSet(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testStore()
	run := &deploy.Run{ID: "r1", State: deploy.RunRunning}
	first, err := s.Put(ctx, run, 0)
	require.NoError(t, err)

	_, recreate := s.Put(ctx, run, 0)
	run.Seq = 2
	second, err := s.Put(ctx, run, first)
	require.NoError(t, err)
	_, stale := s.Put(ctx, run, first)
	got, gotRev, err := s.Get(ctx, "r1")
	require.NoError(t, err)
	_, _, missing := s.Get(ctx, "nope")

	type result struct {
		RecreateConflicts, StaleConflicts, MissingNotFound bool
		Seq                                                uint64
		Rev                                                ports.Revision
	}
	assert.Equal(t,
		result{RecreateConflicts: true, StaleConflicts: true, MissingNotFound: true, Seq: 2, Rev: second},
		result{
			RecreateConflicts: errors.Is(recreate, ports.ErrConflict),
			StaleConflicts:    errors.Is(stale, ports.ErrConflict),
			MissingNotFound:   errors.Is(missing, ports.ErrNotFound),
			Seq:               got.Seq,
			Rev:               gotRev,
		})
}

func activeID(t *testing.T, s *Store) deploy.RunID {
	t.Helper()
	run, _, ok, err := s.Active(context.Background())
	require.NoError(t, err)
	if !ok {
		return ""
	}
	return run.ID
}

func TestActivePointerAllowsOneLiveRun(t *testing.T) {
	ctx := context.Background()
	s, kv, _ := testStore()
	r1 := &deploy.Run{ID: "r1", State: deploy.RunRunning}
	r2 := &deploy.Run{ID: "r2", State: deploy.RunWaiting}

	rev, err := s.Put(ctx, r1, 0)
	require.NoError(t, err)
	whileR1 := activeID(t, s)
	_, second := s.Put(ctx, r2, 0)
	_, _, r2Written := s.Get(ctx, "r2")
	r1.State = deploy.RunFailed
	_, err = s.Put(ctx, r1, rev)
	require.NoError(t, err)
	_, pointerLeft := kv.data[keyActive]
	afterFail := activeID(t, s)
	_, err = s.Put(ctx, r2, 0)
	require.NoError(t, err)

	type result struct {
		WhileR1, AfterFail, AfterR2 deploy.RunID
		SecondConflicts             bool
		R2NotWrittenOnConflict      bool
		PointerLeft                 bool
	}
	assert.Equal(t,
		result{WhileR1: "r1", AfterFail: "", AfterR2: "r2", SecondConflicts: true, R2NotWrittenOnConflict: true},
		result{
			WhileR1: whileR1, AfterFail: afterFail, AfterR2: activeID(t, s),
			SecondConflicts:        errors.Is(second, ports.ErrConflict),
			R2NotWrittenOnConflict: errors.Is(r2Written, ports.ErrNotFound),
			PointerLeft:            pointerLeft,
		})
}

func TestActivePointerStaleHolderIsTakenOver(t *testing.T) {
	cases := []struct {
		name string
		seed func(*fakeKV)
	}{
		{"holder never written", func(kv *fakeKV) {}},
		{"holder terminal", func(kv *fakeKV) {
			kv.seed(runKey("r0"), deploy.Run{ID: "r0", State: deploy.RunCancelled})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, kv, _ := testStore()
			kv.seed(keyActive, deploy.RunID("r0"))
			tc.seed(kv)
			before := activeID(t, s)
			_, err := s.Put(context.Background(), &deploy.Run{ID: "r1", State: deploy.RunRunning}, 0)
			require.NoError(t, err)
			assert.Equal(t, [2]deploy.RunID{"", "r1"}, [2]deploy.RunID{before, activeID(t, s)})
		})
	}
}

func listIDs(t *testing.T, s *Store, limit int) []deploy.RunID {
	t.Helper()
	sums, err := s.List(context.Background(), limit)
	require.NoError(t, err)
	ids := make([]deploy.RunID, 0, len(sums))
	for _, sum := range sums {
		ids = append(ids, sum.ID)
	}
	return ids
}

func TestListNewestFirstAndPrunesPastKeepRuns(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testStore()
	total := deploy.KeepRuns + 2
	for i := range total {
		_, err := s.Put(ctx, &deploy.Run{ID: deploy.RunID(fmt.Sprintf("r%03d", i)), State: deploy.RunSucceeded}, 0)
		require.NoError(t, err)
	}
	_, _, pruned := s.Get(ctx, "r001")
	_, _, kept := s.Get(ctx, "r002")
	all := listIDs(t, s, 0)

	type result struct {
		Len               int
		First, Last       deploy.RunID
		Top3              []deploy.RunID
		OverLimitLen      int
		OldestPruned      bool
		OldestKeptPresent bool
	}
	assert.Equal(t,
		result{
			Len: deploy.KeepRuns, First: "r051", Last: "r002",
			Top3: []deploy.RunID{"r051", "r050", "r049"}, OverLimitLen: deploy.KeepRuns,
			OldestPruned: true, OldestKeptPresent: true,
		},
		result{
			Len: len(all), First: all[0], Last: all[len(all)-1],
			Top3: listIDs(t, s, 3), OverLimitLen: len(listIDs(t, s, 1000)),
			OldestPruned: errors.Is(pruned, ports.ErrNotFound), OldestKeptPresent: kept == nil,
		})
}

func TestListSkipsUnwrittenAndDedupesRetry(t *testing.T) {
	ctx := context.Background()
	s, kv, _ := testStore()
	kv.seed(keyIndex, []deploy.RunID{"r1", "ghost"})
	_, err := s.Put(ctx, &deploy.Run{ID: "r1", State: deploy.RunRunning}, 0)
	require.NoError(t, err)
	assert.Equal(t, []deploy.RunID{"r1"}, listIDs(t, s, 0))
}

func TestListEmptyBucket(t *testing.T) {
	s, _, _ := testStore()
	sums, err := s.List(context.Background(), 0)
	require.NoError(t, err)
	assert.Equal(t, []deploy.RunSummary{}, sums)
}
