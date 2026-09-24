// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"fmt"
	"sync"

	"ItsBagelBot/pkg/env"

	"github.com/newrelic/go-agent/v3/newrelic"
)

var (
	queryGateOnce sync.Once
	queryGate     chan struct{}
)

func newGate(size int) chan struct{} {
	if size <= 0 {
		size = defaultMaxConns
	}
	return make(chan struct{}, size)
}

func gate() chan struct{} {
	queryGateOnce.Do(func() {
		size := env.GetInt("DB_QUERY_CONCURRENCY", env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns))
		queryGate = newGate(size)
	})
	return queryGate
}

func WithQuery[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	release, err := acquire(ctx)
	if err != nil {
		return zero, err
	}
	defer release()
	return fn(ctx)
}

func WithExec(ctx context.Context, fn func(context.Context) error) error {
	_, err := WithQuery(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

const gateWaitSegment = "db.gate.wait"

type gateResult string

const (
	gateAcquired gateResult = "acquired"
	gateTimedOut gateResult = "timeout"
)

func acquire(ctx context.Context) (func(), error) {
	return acquireFrom(ctx, gate())
}

func acquireFrom(ctx context.Context, slots chan struct{}) (func(), error) {
	select {
	case slots <- struct{}{}:
		return releaseSlot(slots), nil
	default:
	}

	segment := startGateSegment(ctx)
	select {
	case slots <- struct{}{}:
		endGateSegment(segment, gateAcquired)
		return releaseSlot(slots), nil
	case <-ctx.Done():
		endGateSegment(segment, gateTimedOut)
		return nil, fmt.Errorf("db concurrency gate: %w", ctx.Err())
	}
}

func releaseSlot(slots chan struct{}) func() {
	return func() { <-slots }
}

func startGateSegment(ctx context.Context) *newrelic.Segment {
	txn := newrelic.FromContext(ctx)
	if txn == nil {
		return nil
	}
	return txn.StartSegment(gateWaitSegment)
}

func endGateSegment(segment *newrelic.Segment, result gateResult) {
	if segment == nil {
		return
	}
	segment.AddAttribute("result", string(result))
	segment.End()
}
