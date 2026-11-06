// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	TxAttempts     = 3
	txRetryBackoff = 10 * time.Millisecond
)

const (
	MySQLLockWaitTimeout = 1205
	MySQLDeadlock        = 1213
)

// Each attempt must begin and roll back its own transaction; after 1213 the server has already discarded every earlier statement.
func RetryTx(ctx context.Context, attempt func(context.Context) error, retryable ...error) error {
	var err error
	for n := range TxAttempts {
		err = attempt(ctx)
		if n == TxAttempts-1 || !RetryableTx(err, retryable) {
			return err
		}
		if waitErr := waitTxRetry(ctx, n); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
	return err
}

func RetryableTx(err error, sentinels []error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == MySQLDeadlock || mysqlErr.Number == MySQLLockWaitTimeout
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
