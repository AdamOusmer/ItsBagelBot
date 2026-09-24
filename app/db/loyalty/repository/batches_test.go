package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

type batchStore struct {
	receipts        map[string]bool
	writes          int
	pendingID       string
	pendingWrites   int
	failWrite       bool
	uncertainCommit bool
}
type batchDriver struct{ store *batchStore }
type batchConn struct{ store *batchStore }
type batchTx struct{ store *batchStore }

func (d batchDriver) Open(string) (driver.Conn, error)   { return &batchConn{d.store}, nil }
func (c *batchConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *batchConn) Close() error                        { return nil }
func (c *batchConn) Begin() (driver.Tx, error) {
	c.store.pendingID = ""
	c.store.pendingWrites = 0
	return &batchTx{c.store}, nil
}
func (c *batchConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT IGNORE INTO counter_batches") {
		id := args[0].Value.(string)
		if c.store.receipts[id] {
			return driver.RowsAffected(0), nil
		}
		c.store.pendingID = id
		return driver.RowsAffected(1), nil
	}
	if c.store.failWrite {
		return nil, errors.New("write failed")
	}
	c.store.pendingWrites++
	return driver.RowsAffected(1), nil
}
func (tx *batchTx) Commit() error {
	tx.store.receipts[tx.store.pendingID] = true
	tx.store.writes += tx.store.pendingWrites
	if tx.store.uncertainCommit {
		return errors.New("commit response lost")
	}
	return nil
}
func (tx *batchTx) Rollback() error { tx.store.pendingID = ""; tx.store.pendingWrites = 0; return nil }
func batchRepo(t *testing.T) (*Loyalty, *batchStore) {
	t.Helper()
	store := &batchStore{receipts: map[string]bool{}}
	name := fmt.Sprintf("counter-batch-%d", fakeDBSeq.Add(1))
	sql.Register(name, batchDriver{store})
	pool, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	return &Loyalty{sqldb: pool}, store
}
func TestCounterBatchRedeliveryAfterUncertainCommit(t *testing.T) {
	repo, store := batchRepo(t)
	dto := channelBump("messages")
	dto.BatchID = "window-1"
	store.uncertainCommit = true
	require.Error(t, repo.ApplyBumps(context.Background(), dto))
	require.Equal(t, 1, store.writes)
	store.uncertainCommit = false
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.Equal(t, 1, store.writes)
	dto.BatchID = "window-2"
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.Equal(t, 2, store.writes)
}
func TestCounterBatchWriteFailureRollsBackReceipt(t *testing.T) {
	repo, store := batchRepo(t)
	dto := channelBump("messages")
	dto.BatchID = "window-1"
	store.failWrite = true
	require.Error(t, repo.ApplyBumps(context.Background(), dto))
	require.Empty(t, store.receipts)
	store.failWrite = false
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.Equal(t, 1, store.writes)
}
func TestCounterBatchRejectsOverflowBeforeDatabase(t *testing.T) {
	repo, store := batchRepo(t)
	dto := channelBump("messages")
	dto.BatchID = "window-1"
	dto.Bumps[0].Delta = data.MaxCounter
	dto.Bumps = append(dto.Bumps, data.CounterBumpEntry{Name: "messages", Delta: 1})
	require.ErrorIs(t, repo.ApplyBumps(context.Background(), dto), ErrInvalidInput)
	require.Empty(t, store.receipts)
}
