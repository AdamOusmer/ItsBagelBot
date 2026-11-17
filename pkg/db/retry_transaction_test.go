// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/pkg/db"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type transactionCounts struct {
	starts, commits, rollbacks int
}

type transactionStarter struct {
	counts    transactionCounts
	committed int
}

type transactionAttempt struct {
	owner   *transactionStarter
	pending int
}

func (s *transactionStarter) Tx(context.Context) (*transactionAttempt, error) {
	s.counts.starts++
	return &transactionAttempt{owner: s}, nil
}

func (tx *transactionAttempt) Commit() error {
	tx.owner.counts.commits++
	tx.owner.committed += tx.pending
	return nil
}

func (tx *transactionAttempt) Rollback() error {
	tx.owner.counts.rollbacks++
	return nil
}

func TestRetryWithTxReplaysWholeTransactions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failure  error
		attempts int
	}{
		{"deadlock", &mysql.MySQLError{Number: 1213}, 2},
		{"lock timeout", &mysql.MySQLError{Number: 1205}, 2},
		{"duplicate", &mysql.MySQLError{Number: 1062}, 1},
		{"ambiguous connection", errors.New("connection lost"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			starter := &transactionStarter{}
			value, err := db.RetryWithTxResult(context.Background(), starter, func(tx *transactionAttempt) (int, error) {
				require.Zero(t, tx.pending, "each attempt starts a fresh transaction")
				tx.pending++
				if starter.counts.starts == 1 {
					return 99, tc.failure
				}
				tx.pending++
				return tx.pending, nil
			})
			if tc.attempts == 1 {
				require.ErrorIs(t, err, tc.failure)
				require.Equal(t, [2]int{}, [2]int{value, starter.committed})
				require.Equal(t, transactionCounts{starts: 1, rollbacks: 1}, starter.counts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, [2]int{2, 2}, [2]int{value, starter.committed})
			require.Equal(t, transactionCounts{starts: 2, commits: 1, rollbacks: 1}, starter.counts)
		})
	}
}

func TestRetryWithTxDiscardsExhaustedResults(t *testing.T) {
	starter := &transactionStarter{}
	value, err := db.RetryWithTxResult(context.Background(), starter, func(tx *transactionAttempt) (int, error) {
		tx.pending++
		return 99, &mysql.MySQLError{Number: 1213}
	})
	require.Error(t, err)
	require.Equal(t, [2]int{}, [2]int{value, starter.committed})
	require.Equal(t, transactionCounts{starts: db.TxAttempts, rollbacks: db.TxAttempts}, starter.counts)
}
