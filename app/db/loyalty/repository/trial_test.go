// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type counterStore struct {
	mu        sync.Mutex
	values    map[string]int64
	locks     []string
	onLock    func(name string)
	deadlocks int
}

func (s *counterStore) takeLocks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	locks := s.locks
	s.locks = nil
	return locks
}

type counterConn struct {
	s       *counterStore
	pending map[string]int64
	held    map[string]bool
}

type counterTx struct{ c *counterConn }

type counterRows struct {
	cols []string
	rows [][]driver.Value
}

func (*counterConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unused") }
func (*counterConn) Close() error                        { return nil }
func (c *counterConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *counterConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.pending = map[string]int64{}
	c.held = map[string]bool{}
	return counterTx{c}, nil
}

func (tx counterTx) Commit() error {
	tx.c.s.mu.Lock()
	defer tx.c.s.mu.Unlock()
	for name, delta := range tx.c.pending {
		tx.c.s.values[name] += delta
	}
	tx.c.pending = nil
	return nil
}

func (tx counterTx) Rollback() error {
	tx.c.pending = nil
	return nil
}

func (c *counterConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	name := args[1].Value.(string)
	switch query {
	case ensureTrialRows:
		for i := 1; i < len(args); i += 4 {
			c.lock(args[i].Value.(string))
			c.pending[args[i].Value.(string)] += 0
		}
		return driver.RowsAffected(0), nil
	case claimTrialPromotion:
		if c.s.deadlocks > 0 {
			c.s.deadlocks--
			return nil, &mysql.MySQLError{Number: mysqlDeadlock}
		}
		c.lock(name)
		if _, ok := c.s.values[name]; ok {
			return driver.RowsAffected(0), nil
		}
		c.pending[name] = 1
		return driver.RowsAffected(1), nil
	case addChannelCounter:
		c.lock(name)
		c.pending[name] += args[2].Value.(int64)
		return driver.RowsAffected(1), nil
	}
	return nil, fmt.Errorf("unexpected exec %q", query)
}

func (c *counterConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	switch query {
	case readTrialSnapshot:
		rows := &counterRows{cols: []string{"name", "value"}}
		for _, arg := range args[1:] {
			if value, ok := c.s.values[arg.Value.(string)]; ok {
				rows.rows = append(rows.rows, []driver.Value{arg.Value, value})
			}
		}
		return rows, nil
	case lockTrialRow:
		return c.lockRow(args[1].Value.(string)), nil
	}
	return nil, fmt.Errorf("unexpected query %q", query)
}

func (c *counterConn) lock(name string) {
	if c.held[name] {
		return
	}
	c.held[name] = true
	c.s.locks = append(c.s.locks, name)
	if c.s.onLock != nil {
		c.s.onLock(name)
	}
}

func (c *counterConn) lockRow(name string) *counterRows {
	c.lock(name)
	rows := &counterRows{cols: []string{"value"}}
	if value, ok := c.s.values[name]; ok {
		rows.rows = append(rows.rows, []driver.Value{value})
	}
	return rows
}

func (r *counterRows) Columns() []string { return r.cols }
func (*counterRows) Close() error        { return nil }
func (r *counterRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

type counterDriver struct{ s *counterStore }

func (d counterDriver) Open(string) (driver.Conn, error) { return &counterConn{s: d.s}, nil }

func promoteRepo(t *testing.T, values map[string]int64) (*Loyalty, *counterStore) {
	t.Helper()
	store := &counterStore{values: values}
	name := fmt.Sprintf("loyalty-trial-%d", fakeDBSeq.Add(1))
	sql.Register(name, counterDriver{store})
	sqldb, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	return &Loyalty{sqldb: sqldb, log: zap.NewNop()}, store
}

func TestPromoteTrialCarriesCountersIntoTheChannelOnce(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{
		data.CounterTrialDecoded:      120,
		data.CounterTrialAnswered:     7,
		data.CounterMessagesProcessed: 5,
	})

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, promoted)
	require.Equal(t, []string{
		data.CounterCommandsAnswered,
		data.CounterEventsProcessed,
		data.CounterMessagesProcessed,
		data.CounterTrialAnswered,
		data.CounterTrialDecoded,
		data.CounterTrialPromoted,
	}, store.takeLocks())

	promoted, err = r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, promoted, "a second promotion must be a no-op")
	require.Empty(t, store.takeLocks(), "a promoted trial must not take row locks")

	require.Equal(t, int64(120), store.values[data.CounterEventsProcessed])
	require.Equal(t, int64(125), store.values[data.CounterMessagesProcessed])
	require.Equal(t, int64(7), store.values[data.CounterCommandsAnswered])
	require.Equal(t, int64(1), store.values[data.CounterTrialPromoted])
}

func TestPromoteTrialLocksCounterRowsInAscendingNameOrder(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{data.CounterTrialDecoded: 3, data.CounterTrialAnswered: 1})

	_, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	locks := store.takeLocks()
	require.NotEmpty(t, locks)
	for i := 1; i < len(locks); i++ {
		require.Less(t, locks[i-1], locks[i], "counters rows must lock in ascending (user_id, name) order")
	}
}

func TestPromoteTrialSkipsEmptyTotals(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{})

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, promoted)
	require.Equal(t, []string{data.CounterTrialAnswered, data.CounterTrialDecoded, data.CounterTrialPromoted}, store.takeLocks())
	require.NotContains(t, store.values, data.CounterEventsProcessed)
	require.NotContains(t, store.values, data.CounterCommandsAnswered)
}

func TestPromoteTrialCarriesABumpCommittedBeforeTheLock(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{data.CounterTrialDecoded: 120, data.CounterMessagesProcessed: 5})
	store.onLock = func(name string) {
		if name == data.CounterTrialDecoded && store.values[name] == 120 {
			store.values[name] += 30
		}
	}

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, promoted)
	require.Equal(t, int64(150), store.values[data.CounterEventsProcessed])
	require.Equal(t, int64(155), store.values[data.CounterMessagesProcessed])
	require.Equal(t, int64(1), store.values[data.CounterTrialPromoted])
}

func TestPromoteTrialGivesUpWhileTotalsKeepMoving(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{data.CounterTrialDecoded: 120})
	store.onLock = func(name string) {
		if name == data.CounterTrialDecoded {
			store.values[name]++
		}
	}

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.ErrorIs(t, err, errTrialTotalsMoved)
	require.False(t, promoted)
	require.NotContains(t, store.values, data.CounterEventsProcessed, "an abandoned promotion must roll back its carry")
	require.NotContains(t, store.values, data.CounterTrialPromoted)
}

func TestPromoteTrialCreatesMissingTrialRowsSoTheyLock(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{data.CounterTrialDecoded: 4})

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, promoted)
	require.Contains(t, store.values, data.CounterTrialAnswered, "an absent trial row takes no lock under READ COMMITTED")
	require.Zero(t, store.values[data.CounterTrialAnswered])
	require.Equal(t, int64(4), store.values[data.CounterTrialDecoded])
}

func TestPromoteTrialRetriesAfterADeadlock(t *testing.T) {
	r, store := promoteRepo(t, map[string]int64{data.CounterTrialDecoded: 9})
	store.deadlocks = 1

	promoted, err := r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, promoted)
	require.Equal(t, int64(9), store.values[data.CounterEventsProcessed], "the rolled-back attempt must not carry twice")
	require.Equal(t, int64(1), store.values[data.CounterTrialPromoted])
}

func TestTrialCountersAreReservedSystemCounters(t *testing.T) {
	require.True(t, data.SystemCounter(data.CounterTrialDecoded))
	require.True(t, data.SystemCounter("trial_blocked"))
	require.False(t, data.SystemCounter("trials"))
	_, err := writableCounterName(42, "trial_blocked")
	require.Error(t, err, "a streamer must not create or set a trial counter")
}
