// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type receiptStore struct {
	mu        sync.Mutex
	createdAt []time.Time
	cutoffs   []time.Time
	limits    []int64
	deadlocks int
	onDelete  func()
}

type receiptConn struct {
	unusedConn
	store *receiptStore
}

func (c *receiptConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if query != pruneBatchReceipts {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	s := c.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deadlocks > 0 {
		s.deadlocks--
		return nil, deadlockError()
	}
	cutoff, limit := args[0].Value.(time.Time), args[1].Value.(int64)
	s.cutoffs = append(s.cutoffs, cutoff)
	s.limits = append(s.limits, limit)
	deleted := s.expiredPrefix(cutoff, int(limit))
	s.createdAt = s.createdAt[deleted:]
	if s.onDelete != nil {
		s.onDelete()
	}
	return driver.RowsAffected(deleted), nil
}

func (s *receiptStore) expiredPrefix(cutoff time.Time, limit int) int {
	window := s.createdAt[:min(limit, len(s.createdAt))]
	for i, createdAt := range window {
		if !createdAt.Before(cutoff) {
			return i
		}
	}
	return len(window)
}

func (s *receiptStore) remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.createdAt)
}

func (s *receiptStore) seenCutoffs() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.cutoffs...)
}

func receiptRepo(t *testing.T, store *receiptStore) *Loyalty {
	t.Helper()
	return fakeLoyalty(t, func() driver.Conn { return &receiptConn{store: store} })
}

func receiptsAt(base time.Time, old, fresh int) []time.Time {
	rows := make([]time.Time, 0, old+fresh)
	for n := range old {
		rows = append(rows, base.Add(-time.Hour-time.Duration(old-n)*time.Second))
	}
	for n := range fresh {
		rows = append(rows, base.Add(time.Duration(n)*time.Second))
	}
	return rows
}

func TestPruneBatchReceipts(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name          string
		old, fresh    int
		deadlocks     int
		cancelOnFirst bool
		wantDeleted   int64
		wantErr       error
		wantRemaining int
		wantCalls     int
	}{
		{name: "TestPruneBatchReceiptsLoopsUntilShortChunk", old: 2*batchReceiptPruneChunk + 500, fresh: 7, wantDeleted: 2*batchReceiptPruneChunk + 500, wantRemaining: 7, wantCalls: 3},
		{name: "TestPruneBatchReceiptsStopsOnExactChunkBoundary", old: batchReceiptPruneChunk, wantDeleted: batchReceiptPruneChunk, wantCalls: 2},
		{name: "TestPruneBatchReceiptsRespectsCancellation", old: 3 * batchReceiptPruneChunk, cancelOnFirst: true, wantDeleted: batchReceiptPruneChunk, wantErr: context.Canceled, wantRemaining: 2 * batchReceiptPruneChunk, wantCalls: 1},
		{name: "TestPruneBatchReceiptsRetriesDeadlock", old: 3, fresh: 2, deadlocks: 1, wantDeleted: 3, wantRemaining: 2, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &receiptStore{createdAt: receiptsAt(cutoff, tc.old, tc.fresh), deadlocks: tc.deadlocks}
			if tc.cancelOnFirst {
				store.onDelete = cancel
			}

			deleted, err := receiptRepo(t, store).PruneBatchReceipts(ctx, cutoff)

			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantDeleted, deleted)
			assert.Equal(t, tc.wantRemaining, store.remaining())
			seen := store.seenCutoffs()
			assert.Len(t, seen, tc.wantCalls)
			for _, got := range seen {
				assert.Equal(t, cutoff, got)
			}
			for _, limit := range store.limits {
				assert.EqualValues(t, batchReceiptPruneChunk, limit)
			}
		})
	}
}

func TestBatchReceiptPrunerUsesRetentionCutoffAndStops(t *testing.T) {
	store := &receiptStore{createdAt: receiptsAt(time.Now().Add(-BatchReceiptRetention), 4, 0)}
	repo := receiptRepo(t, store)

	started := time.Now()
	stop := repo.StartBatchReceiptPruner(context.Background(), time.Millisecond, BatchReceiptRetention)
	require.Eventually(t, func() bool { return store.remaining() == 0 }, time.Second, time.Millisecond)
	stop()

	cutoff := store.seenCutoffs()[0]
	require.False(t, cutoff.Before(started.Add(-BatchReceiptRetention)))
	require.False(t, cutoff.After(time.Now().Add(-BatchReceiptRetention)))
	ticks := len(store.seenCutoffs())
	time.Sleep(5 * time.Millisecond)
	require.Len(t, store.seenCutoffs(), ticks)
}
