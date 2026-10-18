// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
)

type batchStore struct {
	writes          int
	pendingWrites   int
	failWrite       bool
	uncertainCommit bool
	deadlocks       int
	values          map[string]int64
	pendingValues   map[string]int64
	queries         []string
	onShareLock     func(*batchStore)
}

type batchConn struct {
	unusedConn
	store *batchStore
}

type batchTx struct{ store *batchStore }

func (c *batchConn) Begin() (driver.Tx, error) {
	c.store.pendingWrites = 0
	c.store.pendingValues = map[string]int64{}
	return &batchTx{c.store}, nil
}

func (c *batchConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.store.deadlocks > 0 {
		c.store.deadlocks--
		return nil, deadlockError()
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
	tx.store.pendingWrites = 0
	tx.store.pendingValues = nil
	return nil
}

type coalescingConn struct{ *batchConn }

func (c *coalescingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "(?, ?, ?, 0, ?, ?)") {
		c.store.pendingWrites++
		return driver.RowsAffected(0), nil
	}
	return c.batchConn.ExecContext(ctx, query, args)
}

func (c *coalescingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(query, "SELECT user_id,name,value FROM counters") {
		return c.batchConn.QueryContext(ctx, query, args)
	}
	rows := &counterRows{cols: []string{"user_id", "name", "value"}}
	for i := 0; i < len(args); i += 2 {
		name := args[i+1].Value.(string)
		if value, ok := c.store.values[name]; ok {
			rows.rows = append(rows.rows, []driver.Value{args[i].Value, name, value})
		}
	}
	return rows, nil
}

func newBatchStore(values map[string]int64) *batchStore {
	if values == nil {
		values = map[string]int64{}
	}
	return &batchStore{values: values}
}

func batchRepo(t *testing.T) (*Loyalty, *batchStore) {
	t.Helper()
	store := newBatchStore(nil)
	return fakeLoyalty(t, func() driver.Conn { return &batchConn{store: store} }), store
}

func coalescingRepo(t *testing.T, store *batchStore) *Loyalty {
	t.Helper()
	return fakeLoyalty(t, func() driver.Conn { return &coalescingConn{&batchConn{store: store}} })
}

func promoteWhileLocking(s *batchStore) {
	s.values[data.CounterTrialPromoted] = 1
	s.values[data.CounterMessagesProcessed] += 100
	s.values[data.CounterEventsProcessed] += 100
}

func windowBatch(id string) data.CounterBumpedDTO {
	dto := channelBump("messages")
	dto.BatchID = id
	return dto
}

func trialBatch(id string, bumps ...data.CounterBumpEntry) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{BatchID: id, UserID: 42, Bumps: bumps}
}
