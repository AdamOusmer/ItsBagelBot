// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/pkg/db"
)

const (
	txAttempts           = db.TxAttempts
	mysqlLockWaitTimeout = db.MySQLLockWaitTimeout
	mysqlDeadlock        = db.MySQLDeadlock
)

func retryTx(ctx context.Context, attempt func(context.Context) error, retryable ...error) error {
	return db.RetryTx(ctx, attempt, retryable...)
}
