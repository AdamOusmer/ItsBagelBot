// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	unusedConn
	s       *counterStore
	pending map[string]int64
	held    map[string]bool
}

type counterTx struct{ c *counterConn }

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

func promoteRepo(t *testing.T, values map[string]int64) (*Loyalty, *counterStore) {
	t.Helper()
	store := &counterStore{values: values}
	return fakeLoyalty(t, func() driver.Conn { return &counterConn{s: store} }), store
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

	assert.Equal(t, map[string]int64{
		data.CounterTrialDecoded:      120,
		data.CounterTrialAnswered:     7,
		data.CounterEventsProcessed:   120,
		data.CounterMessagesProcessed: 125,
		data.CounterCommandsAnswered:  7,
		data.CounterTrialPromoted:     1,
	}, store.values)
}

type promoteTrialCase struct {
	name         string
	values       map[string]int64
	onLock       func(values map[string]int64, name string)
	deadlocks    int
	wantErr      error
	wantPromoted bool
	wantValues   map[string]int64
	wantAbsent   []string
	wantLocks    []string
}

func promoteTrialCases() []promoteTrialCase {
	return []promoteTrialCase{
		{
			name:         "TestPromoteTrialLocksCounterRowsInAscendingNameOrder",
			values:       map[string]int64{data.CounterTrialDecoded: 3, data.CounterTrialAnswered: 1},
			wantPromoted: true,
			wantLocks: []string{
				data.CounterCommandsAnswered,
				data.CounterEventsProcessed,
				data.CounterMessagesProcessed,
				data.CounterTrialAnswered,
				data.CounterTrialDecoded,
				data.CounterTrialPromoted,
			},
		},
		{
			name:         "TestPromoteTrialSkipsEmptyTotals",
			values:       map[string]int64{},
			wantPromoted: true,
			wantAbsent:   []string{data.CounterEventsProcessed, data.CounterCommandsAnswered},
			wantLocks:    []string{data.CounterTrialAnswered, data.CounterTrialDecoded, data.CounterTrialPromoted},
		},
		{
			name:   "TestPromoteTrialCarriesABumpCommittedBeforeTheLock",
			values: map[string]int64{data.CounterTrialDecoded: 120, data.CounterMessagesProcessed: 5},
			onLock: func(values map[string]int64, name string) {
				if name == data.CounterTrialDecoded && values[name] == 120 {
					values[name] += 30
				}
			},
			wantPromoted: true,
			wantValues: map[string]int64{
				data.CounterEventsProcessed:   150,
				data.CounterMessagesProcessed: 155,
				data.CounterTrialPromoted:     1,
			},
		},
		{
			name:   "TestPromoteTrialGivesUpWhileTotalsKeepMoving",
			values: map[string]int64{data.CounterTrialDecoded: 120},
			onLock: func(values map[string]int64, name string) {
				if name == data.CounterTrialDecoded {
					values[name]++
				}
			},
			wantErr:    errTrialTotalsMoved,
			wantAbsent: []string{data.CounterEventsProcessed, data.CounterTrialPromoted},
		},
		{
			name:         "TestPromoteTrialCreatesMissingTrialRowsSoTheyLock",
			values:       map[string]int64{data.CounterTrialDecoded: 4},
			wantPromoted: true,
			wantValues:   map[string]int64{data.CounterTrialAnswered: 0, data.CounterTrialDecoded: 4},
		},
		{
			name:         "TestPromoteTrialRetriesAfterADeadlock",
			values:       map[string]int64{data.CounterTrialDecoded: 9},
			deadlocks:    1,
			wantPromoted: true,
			wantValues:   map[string]int64{data.CounterEventsProcessed: 9, data.CounterTrialPromoted: 1},
		},
	}
}

func requireStoredTrial(t *testing.T, tc promoteTrialCase, store *counterStore) {
	t.Helper()
	for name, want := range tc.wantValues {
		require.Contains(t, store.values, name)
		assert.Equal(t, want, store.values[name], name)
	}
	for _, name := range tc.wantAbsent {
		assert.NotContains(t, store.values, name, "an abandoned or empty promotion must not carry")
	}
	if tc.wantLocks != nil {
		assert.Equal(t, tc.wantLocks, store.takeLocks())
	}
}

func TestPromoteTrial(t *testing.T) {
	for _, tc := range promoteTrialCases() {
		t.Run(tc.name, func(t *testing.T) {
			r, store := promoteRepo(t, tc.values)
			store.deadlocks = tc.deadlocks
			if tc.onLock != nil {
				store.onLock = func(name string) { tc.onLock(store.values, name) }
			}

			promoted, err := r.PromoteTrial(context.Background(), 42)

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantPromoted, promoted)
			requireStoredTrial(t, tc, store)
		})
	}
}
