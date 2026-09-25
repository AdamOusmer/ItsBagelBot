package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type receiptStore struct {
	mu        sync.Mutex
	createdAt []time.Time
	cutoffs   []time.Time
	limits    []int64
	deadlocks int
	onDelete  func()
}

type receiptDriver struct{ store *receiptStore }
type receiptConn struct{ store *receiptStore }

func (d receiptDriver) Open(string) (driver.Conn, error)   { return &receiptConn{d.store}, nil }
func (c *receiptConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *receiptConn) Close() error                        { return nil }
func (c *receiptConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *receiptConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if query != pruneBatchReceipts {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	s := c.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deadlocks > 0 {
		s.deadlocks--
		return nil, &mysql.MySQLError{Number: mysqlDeadlock, Message: "Deadlock found when trying to get lock"}
	}
	cutoff, limit := args[0].Value.(time.Time), args[1].Value.(int64)
	s.cutoffs = append(s.cutoffs, cutoff)
	s.limits = append(s.limits, limit)
	deleted := 0
	for deleted < int(limit) && deleted < len(s.createdAt) && s.createdAt[deleted].Before(cutoff) {
		deleted++
	}
	s.createdAt = s.createdAt[deleted:]
	if s.onDelete != nil {
		s.onDelete()
	}
	return driver.RowsAffected(deleted), nil
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
	name := fmt.Sprintf("counter-receipts-%d", fakeDBSeq.Add(1))
	sql.Register(name, receiptDriver{store})
	pool, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	return &Loyalty{sqldb: pool, log: zap.NewNop()}
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

func TestPruneBatchReceiptsLoopsUntilShortChunk(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 0)
	store := &receiptStore{createdAt: receiptsAt(cutoff, 2*batchReceiptPruneChunk+500, 7)}
	repo := receiptRepo(t, store)

	deleted, err := repo.PruneBatchReceipts(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(2*batchReceiptPruneChunk+500), deleted)
	require.Equal(t, 7, store.remaining())
	require.Equal(t, []time.Time{cutoff, cutoff, cutoff}, store.seenCutoffs())
	require.Equal(t, []int64{batchReceiptPruneChunk, batchReceiptPruneChunk, batchReceiptPruneChunk}, store.limits)
}

func TestPruneBatchReceiptsStopsOnExactChunkBoundary(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 0)
	store := &receiptStore{createdAt: receiptsAt(cutoff, batchReceiptPruneChunk, 0)}
	repo := receiptRepo(t, store)

	deleted, err := repo.PruneBatchReceipts(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(batchReceiptPruneChunk), deleted)
	require.Len(t, store.seenCutoffs(), 2)
}

func TestPruneBatchReceiptsRespectsCancellation(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &receiptStore{createdAt: receiptsAt(cutoff, 3*batchReceiptPruneChunk, 0), onDelete: cancel}
	repo := receiptRepo(t, store)

	deleted, err := repo.PruneBatchReceipts(ctx, cutoff)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int64(batchReceiptPruneChunk), deleted)
	require.Equal(t, 2*batchReceiptPruneChunk, store.remaining())
}

func TestPruneBatchReceiptsRetriesDeadlock(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 0)
	store := &receiptStore{createdAt: receiptsAt(cutoff, 3, 2), deadlocks: 1}
	repo := receiptRepo(t, store)

	deleted, err := repo.PruneBatchReceipts(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	require.Equal(t, 2, store.remaining())
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
