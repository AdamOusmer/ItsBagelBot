// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueryGateBoundsConcurrentCallers(t *testing.T) {
	t.Setenv("DB_QUERY_CONCURRENCY", "1")
	held, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- WithExec(context.Background(), func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := WithExec(ctx, func(context.Context) error {
		t.Error("query ran while the gate was full")
		return nil
	})
	require.ErrorContains(t, err, "db concurrency gate")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	close(release)
	require.NoError(t, <-holderDone)

	got, err := WithQuery(context.Background(), func(context.Context) (int, error) { return 7, nil })
	require.NoError(t, err)
	require.Equal(t, 7, got, "released slot must admit the next caller")

	failure := errors.New("query failed")
	_, err = WithQuery(context.Background(), func(context.Context) (int, error) { return 0, failure })
	require.ErrorIs(t, err, failure)
	_, err = WithQuery(context.Background(), func(context.Context) (int, error) { return 8, nil })
	require.NoError(t, err, "failed query must still release its slot")
}
