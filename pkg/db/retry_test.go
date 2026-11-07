// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryTxRetriesASentinelWrappingANonRetryableMySQLError(t *testing.T) {
	raced := errors.New("raced")
	duplicate := &mysql.MySQLError{Number: 1062}
	attempts := 0

	err := RetryTx(context.Background(), func(context.Context) error {
		attempts++
		return fmt.Errorf("%w: %w", raced, duplicate)
	}, raced)

	require.ErrorIs(t, err, raced)
	assert.Equal(t, TxAttempts, attempts)
}
