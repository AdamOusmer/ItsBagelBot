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

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type counterStore struct {
	mu     sync.Mutex
	values map[string]int64
}

type counterConn struct{ s *counterStore }
type counterTx struct{}
type counterRows struct {
	vals []int64
	done bool
}

func (counterConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unused") }
func (counterConn) Close() error                        { return nil }
func (counterConn) Begin() (driver.Tx, error)           { return counterTx{}, nil }
func (counterConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return counterTx{}, nil
}
func (counterTx) Commit() error   { return nil }
func (counterTx) Rollback() error { return nil }

func (c counterConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	name := args[1].Value.(string)
	switch query {
	case claimTrialPromotion:
		if _, ok := c.s.values[name]; ok {
			return driver.RowsAffected(0), nil
		}
		c.s.values[name] = 1
		return driver.RowsAffected(1), nil
	case addChannelCounter:
		c.s.values[name] += args[2].Value.(int64)
		return driver.RowsAffected(1), nil
	}
	return nil, fmt.Errorf("unexpected exec %q", query)
}

func (c counterConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query != readTrialTotals {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return &counterRows{vals: []int64{c.s.values[data.CounterTrialDecoded], c.s.values[data.CounterTrialAnswered]}}, nil
}

func (*counterRows) Columns() []string { return []string{"decoded", "answered"} }
func (*counterRows) Close() error      { return nil }
func (r *counterRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0], dest[1] = r.vals[0], r.vals[1]
	return nil
}

type counterDriver struct{ s *counterStore }

func (d counterDriver) Open(string) (driver.Conn, error) { return counterConn{d.s}, nil }

var counterDriverSeq sync.Map

func promoteRepo(t *testing.T, values map[string]int64) (*Loyalty, *counterStore) {
	t.Helper()
	store := &counterStore{values: values}
	name := fmt.Sprintf("loyalty-trial-%s", t.Name())
	if _, loaded := counterDriverSeq.LoadOrStore(name, true); !loaded {
		sql.Register(name, counterDriver{store})
	}
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
	promoted, err = r.PromoteTrial(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, promoted, "a second promotion must be a no-op")

	require.Equal(t, int64(120), store.values[data.CounterEventsProcessed])
	require.Equal(t, int64(125), store.values[data.CounterMessagesProcessed])
	require.Equal(t, int64(7), store.values[data.CounterCommandsAnswered])
	require.Equal(t, int64(1), store.values[data.CounterTrialPromoted])
}

func TestTrialCountersAreReservedSystemCounters(t *testing.T) {
	require.True(t, data.SystemCounter(data.CounterTrialDecoded))
	require.True(t, data.SystemCounter("trial_blocked"))
	require.False(t, data.SystemCounter("trials"))
	_, err := writableCounterName(42, "trial_blocked")
	require.Error(t, err, "a streamer must not create or set a trial counter")
}
