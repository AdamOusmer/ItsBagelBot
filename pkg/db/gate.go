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

// newGate builds the semaphore. Split out of gate() so a test can construct a
// gate of a chosen size without going through the process-wide sync.Once and
// the environment it reads: gate() itself has to keep its exact semantics
// (one gate per process, sized once at first use) because every WithQuery in
// the fleet shares it.
func newGate(size int) chan struct{} {
	if size <= 0 {
		size = defaultMaxConns
	}
	return make(chan struct{}, size)
}

func gate() chan struct{} {
	queryGateOnce.Do(func() {
		// defaultMaxConns is the shared fallback with openPool's
		// SetMaxOpenConns (pkg/db/pool.go) - see its doc comment in
		// provider.go for the server-headroom math behind its value.
		size := env.GetInt("DB_QUERY_CONCURRENCY", env.GetInt("DB_MAX_OPEN_CONNS", defaultMaxConns))
		queryGate = newGate(size)
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

// gateWaitSegment names the New Relic segment that covers time spent waiting
// for the semaphore, and only that time.
//
// Measurement that produced it (loyalty flush, 2026-09-07, production): one
// transaction ran 1,060ms end to end while the INSERT inside it reported
// 3.5ms server-side. The missing 1,057ms was this gate. It was invisible:
// acquire blocked with no metric, no log and no segment, and it sits outside
// the datastore segment nrmysql opens, so New Relic attributed the whole
// second to unaccounted transaction time. A gate that can hold a request for
// a second has to be able to say so, or the next person reads the same trace
// and concludes the database is slow when the pod is simply oversubscribed
// against DB_QUERY_CONCURRENCY.
//
// Alternatives considered. (1) Always time the acquire and record a duration
// metric: rejected, that puts two clock reads and an agent call on the hot,
// uncontended path taken by every query in the fleet. (2) Widen the existing
// datastore segment to cover the wait: rejected, it would silently inflate
// every "MySQL" number in APM with time MySQL never spent, which is exactly
// the misattribution being fixed. (3) Sample like traceValkeyCall does
// (pkg/valkey/telemetry.go, which skips unsampled transactions): rejected
// here, the slow path is by definition rare and each occurrence is the whole
// signal, so dropping most of them would defeat the purpose. The style is
// otherwise mirrored: fixed segment name, one low-cardinality "result"
// attribute, no keys or query text.
//
// The fast path below must therefore stay segment-free and clock-free: it
// takes the non-blocking send and returns, doing no more work than the
// version that had no telemetry at all.
const gateWaitSegment = "db.gate.wait"

// gateResult is the low-cardinality outcome recorded on the wait segment. A
// named type rather than a bare string so a caller cannot pass arbitrary text
// into an APM facet.
type gateResult string

const (
	gateAcquired gateResult = "acquired"
	gateTimedOut gateResult = "timeout"
)

func acquire(ctx context.Context) (func(), error) {
	return acquireFrom(ctx, gate())
}

// acquireFrom takes a slot, returning the release func. Two paths on purpose:
// the uncontended send costs one select, and only a request that actually has
// to wait pays for the segment. See gateWaitSegment for the measurement.
//
// It is separate from acquire so the waiting behaviour can be exercised
// against a gate of a known size, rather than against the process-wide one
// whose capacity comes from the environment.
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

// startGateSegment returns nil when there is no transaction on ctx, which is
// the background/consumer case; endGateSegment then no-ops, keeping acquire
// free of nil checks.
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
