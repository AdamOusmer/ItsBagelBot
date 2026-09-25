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
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type batchStore struct {
	receipts        map[string]bool
	writes          int
	pendingID       string
	pendingWrites   int
	failWrite       bool
	uncertainCommit bool
	deadlocks       int
	values          map[string]int64
	pendingValues   map[string]int64
	queries         []string
	onShareLock     func(*batchStore)
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
	c.store.pendingValues = map[string]int64{}
	return &batchTx{c.store}, nil
}
func (c *batchConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT IGNORE INTO counter_batches") {
		return c.store.stageReceipt(args[0].Value.(string)), nil
	}
	if c.store.deadlocks > 0 {
		c.store.deadlocks--
		return nil, &mysql.MySQLError{Number: mysqlDeadlock, Message: "Deadlock found when trying to get lock"}
	}
	if c.store.failWrite {
		return nil, errors.New("write failed")
	}
	c.store.pendingWrites++
	if strings.HasPrefix(query, "INSERT INTO counters ") {
		c.store.stageCounterRows(args)
	}
	return driver.RowsAffected(1), nil
}
func (s *batchStore) stageReceipt(id string) driver.Result {
	if s.receipts[id] {
		return driver.RowsAffected(0)
	}
	s.pendingID = id
	return driver.RowsAffected(1)
}
func (s *batchStore) stageCounterRows(args []driver.NamedValue) {
	for row := 0; row+5 < len(args); row += 6 {
		s.pendingValues[args[row+1].Value.(string)] += args[row+3].Value.(int64)
	}
}
func (c *batchConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.store.queries = append(c.store.queries, query)
	if query == lockTrialPromotion && c.store.onShareLock != nil {
		c.store.onShareLock(c.store)
		c.store.onShareLock = nil
	}
	rows := &counterRows{cols: []string{"1"}}
	if _, ok := c.store.values[data.CounterTrialPromoted]; ok {
		rows.rows = [][]driver.Value{{int64(1)}}
	}
	return rows, nil
}
func (tx *batchTx) Commit() error {
	tx.store.receipts[tx.store.pendingID] = true
	tx.store.writes += tx.store.pendingWrites
	for name, delta := range tx.store.pendingValues {
		tx.store.values[name] += delta
	}
	if tx.store.uncertainCommit {
		return errors.New("commit response lost")
	}
	return nil
}
func (tx *batchTx) Rollback() error {
	tx.store.pendingID = ""
	tx.store.pendingWrites = 0
	tx.store.pendingValues = nil
	return nil
}
func batchRepo(t *testing.T) (*Loyalty, *batchStore) {
	t.Helper()
	store := &batchStore{receipts: map[string]bool{}, values: map[string]int64{}}
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
func TestCounterBatchRetriesAfterADeadlock(t *testing.T) {
	repo, store := batchRepo(t)
	dto := channelBump("messages")
	dto.BatchID = "window-1"
	store.deadlocks = 1
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.True(t, store.receipts["window-1"])
	require.Equal(t, 1, store.writes)
	require.Equal(t, int64(1), store.values["messages"])
}
func trialBatch(id string, bumps ...data.CounterBumpEntry) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{BatchID: id, UserID: 42, Bumps: bumps}
}
func TestCounterBatchRedirectsLateTrialBumpsOnce(t *testing.T) {
	repo, store := batchRepo(t)
	store.values = map[string]int64{
		data.CounterTrialPromoted:     1,
		data.CounterTrialDecoded:      100,
		data.CounterTrialAnswered:     7,
		data.CounterEventsProcessed:   100,
		data.CounterMessagesProcessed: 100,
		data.CounterCommandsAnswered:  7,
	}
	dto := trialBatch("late-1",
		data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 5},
		data.CounterBumpEntry{Name: data.CounterTrialAnswered, Delta: 2},
		data.CounterBumpEntry{Name: data.CounterEventsProcessed, Delta: 3},
	)
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.NoError(t, repo.ApplyBumps(context.Background(), dto), "redelivery must not redirect again")
	require.Equal(t, []string{readTrialPromotion}, store.queries)
	require.Equal(t, int64(108), store.values[data.CounterEventsProcessed])
	require.Equal(t, int64(105), store.values[data.CounterMessagesProcessed])
	require.Equal(t, int64(9), store.values[data.CounterCommandsAnswered])
	require.Equal(t, int64(105), store.values[data.CounterTrialDecoded], "trial stats keep the late bump")
	require.Equal(t, int64(9), store.values[data.CounterTrialAnswered])
}
func TestCounterBatchRerunsWhenPromotionCommitsWhileItWaits(t *testing.T) {
	repo, store := batchRepo(t)
	store.values = map[string]int64{data.CounterTrialDecoded: 100}
	store.onShareLock = func(s *batchStore) {
		s.values[data.CounterTrialPromoted] = 1
		s.values[data.CounterEventsProcessed] += 100
		s.values[data.CounterMessagesProcessed] += 100
	}
	require.NoError(t, repo.ApplyBumps(context.Background(), trialBatch("race-1", data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 5})))
	require.Equal(t, []string{readTrialPromotion, lockTrialPromotion, readTrialPromotion}, store.queries)
	require.Equal(t, 1, len(store.receipts))
	require.Equal(t, int64(105), store.values[data.CounterEventsProcessed])
	require.Equal(t, int64(105), store.values[data.CounterMessagesProcessed])
	require.Equal(t, int64(105), store.values[data.CounterTrialDecoded])
}
func TestCounterBatchCountsUnpromotedTrialBumpsOnlyAsTrial(t *testing.T) {
	repo, store := batchRepo(t)
	require.NoError(t, repo.ApplyBumps(context.Background(), trialBatch("trial-1", data.CounterBumpEntry{Name: data.CounterTrialAnswered, Delta: 1})))
	require.Equal(t, []string{readTrialPromotion, lockTrialPromotion}, store.queries)
	require.Equal(t, map[string]int64{data.CounterTrialAnswered: 1}, store.values)
}
func TestCounterBatchWithoutTrialBumpsSkipsThePromotionCheck(t *testing.T) {
	repo, store := batchRepo(t)
	store.values[data.CounterTrialPromoted] = 1
	dto := trialBatch("plain-1",
		data.CounterBumpEntry{Name: data.CounterMessagesProcessed, Delta: 1},
		data.CounterBumpEntry{Name: data.TrialCounterPrefix + "processed", Delta: 1},
		data.CounterBumpEntry{Name: data.CounterTrialDecoded, Delta: 3, Scope: data.CounterScopeCommand, Command: "hug"},
	)
	require.NoError(t, repo.ApplyBumps(context.Background(), dto))
	require.Empty(t, store.queries)
	require.Equal(t, int64(1), store.values[data.CounterMessagesProcessed])
	require.NotContains(t, store.values, data.CounterEventsProcessed)
}
