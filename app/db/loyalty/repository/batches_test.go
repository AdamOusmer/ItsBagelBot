// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
)

type batchStore struct {
	values        map[string]int64
	pendingValues map[string]int64
	receipts      [][]driver.Value
	onTrialVerify func(*batchStore)
}

type batchConn struct {
	unusedConn
	store *batchStore
}

type batchTx struct{ store *batchStore }

func (c *batchConn) Begin() (driver.Tx, error) {
	c.store.pendingValues = map[string]int64{}
	c.store.receipts = nil
	return &batchTx{c.store}, nil
}

func (c *batchConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
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

func (c *batchConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(query, "SELECT f.user_id,f.deleted,"):
		return c.store.fenceRows(args), nil
	case strings.HasPrefix(query, "SELECT user_id FROM counters WHERE name='trial_promoted'"):
		return c.store.verifyTrial(args[0].Value), nil
	default:
		return nil, fmt.Errorf("unexpected counter query %q", query)
	}
}

func (s *batchStore) fenceRows(args []driver.NamedValue) *counterRows {
	rows := &counterRows{cols: []string{"user_id", "deleted", "promoted"}}
	_, promoted := s.values[data.CounterTrialPromoted]
	for _, arg := range args {
		rows.rows = append(rows.rows, []driver.Value{arg.Value, false, promoted})
	}
	return rows
}

func (s *batchStore) verifyTrial(id driver.Value) *counterRows {
	if s.onTrialVerify != nil {
		s.onTrialVerify(s)
		s.onTrialVerify = nil
	}
	rows := &counterRows{cols: []string{"user_id"}}
	if _, ok := s.values[data.CounterTrialPromoted]; ok {
		rows.rows = [][]driver.Value{{id}}
	}
	return rows
}

func (tx *batchTx) Commit() error {
	for name, delta := range tx.store.pendingValues {
		tx.store.values[name] += delta
	}
	return nil
}

func (tx *batchTx) Rollback() error {
	tx.store.pendingValues = nil
	return nil
}

type coalescingConn struct{ *batchConn }

func (c *coalescingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO loyalty_counter_receipts ") {
		c.store.stageReceipts(args)
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(query, "(?, ?, ?, 0, ?, ?)") {
		return driver.RowsAffected(0), nil
	}
	return c.batchConn.ExecContext(ctx, query, args)
}

func (s *batchStore) stageReceipts(args []driver.NamedValue) {
	for i := 0; i < len(args); i += 5 {
		s.receipts = append(s.receipts, []driver.Value{args[i].Value, args[i+1].Value, args[i+2].Value, args[i+3].Value})
	}
}

func (c *coalescingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, "SELECT user_id,batch_id,payload_hash,admission_id ") {
		return &counterRows{cols: []string{"user_id", "batch_id", "payload_hash", "admission_id"}, rows: c.store.receipts}, nil
	}
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

func coalescingRepo(t *testing.T, store *batchStore) *Loyalty {
	t.Helper()
	return fakeLoyalty(t, func() driver.Conn { return &coalescingConn{&batchConn{store: store}} })
}

func promoteBeforeTrialVerify(s *batchStore) {
	s.values[data.CounterTrialPromoted] = 1
	s.values[data.CounterMessagesProcessed] += 100
	s.values[data.CounterEventsProcessed] += 100
}

func trialBatch(id string, bumps ...data.CounterBumpEntry) data.CounterBumpedDTO {
	return data.CounterBumpedDTO{BatchID: id, UserID: 42, Bumps: bumps}
}
