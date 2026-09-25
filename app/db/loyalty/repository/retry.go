// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	txAttempts     = 3
	txRetryBackoff = 10 * time.Millisecond
)

const (
	mysqlLockWaitTimeout = 1205
	mysqlDeadlock        = 1213
)

// Each attempt must begin and roll back its own transaction; after 1213 the server has already discarded every earlier statement.
func retryTx(ctx context.Context, attempt func(context.Context) error, retryable ...error) error {
	var err error
	for n := range txAttempts {
		err = attempt(ctx)
		if n == txAttempts-1 || !retryableTx(err, retryable) {
			return err
		}
		if waitErr := waitTxRetry(ctx, n); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
	return err
}

func retryableTx(err error, sentinels []error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == mysqlDeadlock || mysqlErr.Number == mysqlLockWaitTimeout
	}
	return slices.ContainsFunc(sentinels, func(target error) bool { return errors.Is(err, target) })
}

func waitTxRetry(ctx context.Context, attempt int) error {
	backoff := txRetryBackoff << attempt
	timer := time.NewTimer(backoff + rand.N(backoff))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
