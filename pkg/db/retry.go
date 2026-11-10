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

// Tx is what every ent transaction offers.
type Tx interface {
	Commit() error
	Rollback() error
}

// TxStarter is an ent client.
type TxStarter[T Tx] interface {
	Tx(ctx context.Context) (T, error)
}

// WithTx commits when fn succeeds and rolls back when it fails.
func WithTx[T Tx](ctx context.Context, client TxStarter[T], fn func(tx T) error) error {
	_, err := WithTxResult(ctx, client, func(tx T) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

func WithTxResult[R any, T Tx](ctx context.Context, client TxStarter[T], fn func(tx T) (R, error)) (R, error) {
	var zero R
	tx, err := client.Tx(ctx)
	if err != nil {
		return zero, err
	}
	out, err := fn(tx)
	if err != nil {
		_ = tx.Rollback()
		return zero, err
	}
	return out, tx.Commit()
}

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
	// First: a duplicate-key error wrapped with a sentinel also unwraps to its MySQL error.
	if slices.ContainsFunc(sentinels, func(target error) bool { return errors.Is(err, target) }) {
		return true
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == MySQLDeadlock || mysqlErr.Number == MySQLLockWaitTimeout)
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
