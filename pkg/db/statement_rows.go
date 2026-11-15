// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"errors"
	"time"
)

const (
	StatementRows       = 500
	StatementBytes      = 256 * 1024
	StatementParameters = 60000
)

var ErrStatementRowTooLarge = errors.New("SQL row exceeds statement budget")

func StatementRowChunks(rows [][]any, overhead int) ([][][]any, error) {
	var chunks [][][]any
	for len(rows) > 0 {
		n, err := statementRowCount(rows, overhead)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, rows[:n])
		rows = rows[n:]
	}
	return chunks, nil
}

func statementRowCount(rows [][]any, overhead int) (int, error) {
	bytes, parameters := overhead, 0
	for i, row := range rows {
		size := statementRowBytes(row)
		if statementBudgetExceeded(i+1, bytes+size, parameters+len(row)) {
			return fullStatementRows(i)
		}
		bytes += size
		parameters += len(row)
	}
	return len(rows), nil
}

func fullStatementRows(n int) (int, error) {
	if n == 0 {
		return 0, ErrStatementRowTooLarge
	}
	return n, nil
}

func statementBudgetExceeded(rows, bytes, parameters int) bool {
	return rows > StatementRows || bytes > StatementBytes || parameters > StatementParameters
}

func statementRowBytes(row []any) int {
	size := 16
	for _, value := range row {
		size += statementValueBytes(value)
	}
	return size
}

func statementValueBytes(value any) int {
	switch v := value.(type) {
	case string:
		return 2*len(v) + 16
	case []byte:
		return 2*len(v) + 16
	case time.Time:
		return 64
	default:
		return 32
	}
}
