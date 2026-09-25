// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func failingAttempts(failures int, failure error) (func(context.Context) error, *int) {
	calls := 0
	return func(context.Context) error {
		calls++
		if calls <= failures {
			return failure
		}
		return nil
	}, &calls
}

func TestRetryTxRerunsLockConflicts(t *testing.T) {
	for _, failure := range []error{
		&mysql.MySQLError{Number: mysqlDeadlock},
		&mysql.MySQLError{Number: mysqlLockWaitTimeout},
		fmt.Errorf("flush counters: %w", &mysql.MySQLError{Number: mysqlDeadlock}),
	} {
		attempt, calls := failingAttempts(1, failure)
		require.NoError(t, retryTx(context.Background(), attempt), failure.Error())
		require.Equal(t, 2, *calls, failure.Error())
	}
}

func TestRetryTxReturnsOtherErrorsImmediately(t *testing.T) {
	for _, failure := range []error{
		&mysql.MySQLError{Number: 1062},
		errors.New("commit response lost"),
		context.DeadlineExceeded,
	} {
		attempt, calls := failingAttempts(1, failure)
		require.ErrorIs(t, retryTx(context.Background(), attempt), failure)
		require.Equal(t, 1, *calls, failure.Error())
	}
}

func TestRetryTxRerunsNamedSentinels(t *testing.T) {
	sentinel := errors.New("moved")
	attempt, calls := failingAttempts(1, sentinel)
	require.NoError(t, retryTx(context.Background(), attempt, sentinel))
	require.Equal(t, 2, *calls)
}

func TestRetryTxGivesUpAfterBoundedAttempts(t *testing.T) {
	deadlock := &mysql.MySQLError{Number: mysqlDeadlock}
	attempt, calls := failingAttempts(txAttempts+1, deadlock)
	require.ErrorIs(t, retryTx(context.Background(), attempt), deadlock)
	require.Equal(t, txAttempts, *calls)
}

func TestRetryTxStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlock := &mysql.MySQLError{Number: mysqlDeadlock}
	attempt, calls := failingAttempts(txAttempts, deadlock)
	err := retryTx(ctx, attempt)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, deadlock)
	require.Equal(t, 1, *calls)
}
