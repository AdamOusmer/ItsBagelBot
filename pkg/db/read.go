// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
)

const readAttemptCap = time.Second

// fn may run twice, so it must be one idempotent read outside a transaction that fully consumes its rows.
func WithRead[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	return WithQuery(ctx, func(ctx context.Context) (T, error) {
		value, err := firstReadAttempt(ctx, fn)
		if !retryableRead(ctx, err) {
			return value, err
		}
		return fn(ctx)
	})
}

func firstReadAttempt[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	attempt, cancel := context.WithTimeout(ctx, readAttemptTimeout(ctx))
	defer cancel()
	return fn(attempt)
}

func readAttemptTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return readAttemptCap
	}
	return min(readAttemptCap, time.Until(deadline)/2)
}

func retryableRead(parent context.Context, err error) bool {
	if err == nil || parent.Err() != nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || lostConnection(err)
}

func lostConnection(err error) bool {
	if errors.Is(err, mysql.ErrInvalidConn) || errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}
