// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"strings"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

type counterSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type counterTransaction struct {
	counterSQL
	mysql bool
}

type counterAdmission struct {
	inbox        counterInbox
	events       []data.CounterBumpedDTO
	fenceChunks  [][][]any
	receiptRows  [][]any
	receiptStmts []chunkStmt
}

type admittedCounters struct {
	events []data.CounterBumpedDTO
	fences map[uint64]counterFence
}

func (r *Loyalty) prepareCounterAdmission(events []data.CounterBumpedDTO) (counterAdmission, error) {
	inbox, err := newCounterInbox(events)
	if err != nil {
		return counterAdmission{}, err
	}
	fences, err := counterFenceChunks(counterEventUserIDs(events))
	if err != nil {
		return counterAdmission{}, err
	}
	rows := inbox.orderedRows()
	stmts, err := r.counterReceiptInserts(rows)
	return counterAdmission{inbox: inbox, events: events, fenceChunks: fences, receiptRows: rows, receiptStmts: stmts}, err
}

func (r *Loyalty) commitCounterEvents(ctx context.Context, events []data.CounterBumpedDTO) error {
	admission, err := r.prepareCounterAdmission(events)
	if err != nil {
		return err
	}
	if r.dialect == "sqlite3" {
		return r.commitSQLiteCounters(ctx, admission)
	}
	return r.commitMySQLCounters(ctx, admission)
}

func (r *Loyalty) commitSQLiteCounters(ctx context.Context, admission counterAdmission) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.runCounterTransaction(ctx, counterTransaction{counterSQL: tx}, admission); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Loyalty) commitMySQLCounters(ctx context.Context, admission counterAdmission) error {
	conn, err := r.sqldb.Conn(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			db.RollbackConn(ctx, conn)
		}
		_ = conn.Close()
	}()
	err = r.runCounterTransaction(ctx, counterTransaction{counterSQL: conn, mysql: true}, admission)
	committed = err == nil
	return err
}

func (r *Loyalty) runCounterTransaction(ctx context.Context, tx counterTransaction, plan counterAdmission) error {
	batch, err := r.admitCounterEvents(ctx, tx, plan)
	if err != nil {
		return err
	}
	return r.applyAdmittedCounters(ctx, tx, batch)
}

func (r *Loyalty) admitCounterEvents(ctx context.Context, tx counterTransaction, plan counterAdmission) (admittedCounters, error) {
	fences, inserted, err := r.startCounterAdmission(ctx, tx, plan)
	if err != nil {
		return admittedCounters{}, err
	}
	if inserted != int64(len(plan.receiptRows)) {
		err = r.classifyCounterReceipts(ctx, tx, plan.inbox, plan.receiptRows)
	}
	return admittedCounters{events: plan.inbox.admitted(plan.events), fences: fences}, err
}

func (r *Loyalty) startCounterAdmission(ctx context.Context, tx counterTransaction, plan counterAdmission) (map[uint64]counterFence, int64, error) {
	if !tx.mysql {
		return r.admitCounterSequentially(ctx, tx, plan)
	}
	if len(plan.fenceChunks) > 1 || len(plan.receiptStmts) != 1 {
		return r.restartCounterAdmission(ctx, tx, plan, chunkStmt{sql: "START TRANSACTION"})
	}
	fences, inserted, err := r.pipelineCounterAdmission(ctx, tx, plan)
	if err != nil || len(fences) == plan.fenceCount() {
		return fences, inserted, err
	}
	// Holding receipts while creating a missing fence can deadlock against a writer that locked the fence first.
	return r.restartCounterAdmission(ctx, tx, plan, chunkStmt{sql: "ROLLBACK; START TRANSACTION"})
}

func (p counterAdmission) fenceCount() int {
	if len(p.fenceChunks) == 0 {
		return 0
	}
	return len(p.fenceChunks[0])
}

func (r *Loyalty) restartCounterAdmission(ctx context.Context, tx counterSQL, plan counterAdmission, start chunkStmt) (map[uint64]counterFence, int64, error) {
	if _, err := tx.ExecContext(ctx, start.sql); err != nil {
		return nil, 0, err
	}
	return r.admitCounterSequentially(ctx, tx, plan)
}

func (r *Loyalty) admitCounterSequentially(ctx context.Context, tx counterSQL, plan counterAdmission) (map[uint64]counterFence, int64, error) {
	fences, err := r.lockCounterFenceChunks(ctx, tx, plan.fenceChunks)
	if err != nil {
		return nil, 0, err
	}
	var inserted int64
	for _, stmt := range plan.receiptStmts {
		count, err := insertCounterReceipts(ctx, tx, stmt)
		if err != nil {
			return nil, 0, err
		}
		inserted += count
	}
	return fences, inserted, nil
}

func insertCounterReceipts(ctx context.Context, tx counterSQL, stmt chunkStmt) (int64, error) {
	result, err := tx.ExecContext(ctx, stmt.sql, stmt.args...)
	if err != nil {
		return 0, counterWriteError(err)
	}
	return result.RowsAffected()
}

func (r *Loyalty) pipelineCounterAdmission(ctx context.Context, tx counterSQL, plan counterAdmission) (map[uint64]counterFence, int64, error) {
	stmts := []chunkStmt{{sql: "START TRANSACTION"}}
	if len(plan.fenceChunks) != 0 {
		stmts = append(stmts, r.counterFenceRead(plan.fenceChunks[0]))
	}
	stmts = append(stmts, plan.receiptStmts[0], chunkStmt{sql: "SELECT ROW_COUNT()"})
	packet := counterPacket(stmts)
	rows, err := tx.QueryContext(ctx, packet.sql, packet.args...)
	if err != nil {
		return nil, 0, counterWriteError(err)
	}
	defer rows.Close()
	return scanCounterAdmission(rows, len(plan.fenceChunks) != 0)
}

func scanCounterAdmission(rows *sql.Rows, hasFences bool) (map[uint64]counterFence, int64, error) {
	fences := make(map[uint64]counterFence)
	if err := scanCounterAdmissionFences(rows, fences, hasFences); err != nil {
		return nil, 0, err
	}
	if !rows.Next() {
		return nil, 0, counterAdmissionRowsError(rows)
	}
	var inserted int64
	if err := rows.Scan(&inserted); err != nil {
		return nil, 0, err
	}
	return fences, inserted, rows.Close()
}

func scanCounterAdmissionFences(rows *sql.Rows, fences map[uint64]counterFence, hasFences bool) error {
	if !hasFences {
		return nil
	}
	if err := scanCounterFences(rows, fences); err != nil {
		return err
	}
	if !rows.NextResultSet() {
		return counterAdmissionRowsError(rows)
	}
	return nil
}

func counterAdmissionRowsError(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		return counterWriteError(err)
	}
	return sql.ErrNoRows
}

func counterPacket(stmts []chunkStmt) chunkStmt {
	var sql strings.Builder
	var args []any
	for i, stmt := range stmts {
		if i > 0 {
			sql.WriteString("; ")
		}
		sql.WriteString(stmt.sql)
		args = append(args, stmt.args...)
	}
	return chunkStmt{sql: sql.String(), args: args}
}
