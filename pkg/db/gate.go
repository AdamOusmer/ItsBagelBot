package db

import (
	"context"
	"fmt"
	"sync"

	"ItsBagelBot/pkg/env"
)

var (
	queryGateOnce sync.Once
	queryGate     chan struct{}
)

func gate() chan struct{} {
	queryGateOnce.Do(func() {
		size := env.GetInt("DB_QUERY_CONCURRENCY", env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns))
		if size <= 0 {
			size = defaultMaxConns
		}
		queryGate = make(chan struct{}, size)
	})
	return queryGate
}

// WithQuery bounds concurrent database work inside a process. database/sql
// still owns the hard connection cap; this gate prevents request goroutines
// from piling up behind the pool during dashboard/admin bursts.
func WithQuery[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	release, err := acquire(ctx)
	if err != nil {
		return zero, err
	}
	defer release()
	return fn(ctx)
}

// WithExec is the no-result form of WithQuery.
func WithExec(ctx context.Context, fn func(context.Context) error) error {
	_, err := WithQuery(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

func acquire(ctx context.Context) (func(), error) {
	select {
	case gate() <- struct{}{}:
		return func() { <-gate() }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("db concurrency gate: %w", ctx.Err())
	}
}
