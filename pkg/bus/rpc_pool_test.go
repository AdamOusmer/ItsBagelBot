// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

const rpcPoolTestTimeout = 5 * time.Second

type overlapProbe struct {
	arrived   chan struct{}
	release   chan struct{}
	running   atomic.Int64
	peak      atomic.Int64
	completed atomic.Int64
}

func newOverlapProbe(capacity int) *overlapProbe {
	return &overlapProbe{
		arrived: make(chan struct{}, capacity),
		release: make(chan struct{}),
	}
}

func (o *overlapProbe) handle(*nats.Msg) {
	o.recordPeak(o.running.Add(1))
	o.arrived <- struct{}{}
	<-o.release
	o.running.Add(-1)
	o.completed.Add(1)
}

func (o *overlapProbe) recordPeak(current int64) {
	for {
		peak := o.peak.Load()
		if current <= peak || o.peak.CompareAndSwap(peak, current) {
			return
		}
	}
}

func (o *overlapProbe) awaitArrivals(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(rpcPoolTestTimeout)
	for i := 0; i < n; i++ {
		select {
		case <-o.arrived:
		case <-deadline:
			t.Fatalf("only %d of %d handlers started; concurrency is capped below the policy", i, n)
		}
	}
}

func submitAll(pool *RPCPool, n int, handler nats.MsgHandler) *sync.WaitGroup {
	callback := pool.callback(handler)
	var senders sync.WaitGroup
	for i := 0; i < n; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			callback(&nats.Msg{Subject: "bagel.rpc.test"})
		}()
	}
	return &senders
}

func drainAsync(pool *RPCPool) <-chan error {
	drained := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), rpcPoolTestTimeout)
		defer cancel()
		drained <- pool.Drain(ctx)
	}()
	return drained
}

func requireStillDraining(t *testing.T, drained <-chan error) {
	t.Helper()
	select {
	case err := <-drained:
		t.Fatalf("Drain() returned %v while handlers were still running", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func requireDrained(t *testing.T, drained <-chan error) {
	t.Helper()
	select {
	case err := <-drained:
		if err != nil {
			t.Fatalf("Drain() = %v", err)
		}
	case <-time.After(rpcPoolTestTimeout):
		t.Fatal("Drain() never returned after the handlers finished")
	}
}

func waitGroupWithin(t *testing.T, group *sync.WaitGroup, what string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(rpcPoolTestTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func drainWithin(t *testing.T, pool *RPCPool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), rpcPoolTestTimeout)
	defer cancel()
	if err := pool.Drain(ctx); err != nil {
		t.Fatalf("Drain() = %v", err)
	}
}

func TestRPCPoolRunsHandlersConcurrently(t *testing.T) {
	tests := []struct {
		name       string
		policy     RPCPoolPolicy
		messages   int
		wantAtOnce int
	}{
		{
			name:       "four workers overlap four messages",
			policy:     RPCPoolPolicy{MaxWorkers: 4, QueueDepth: 4},
			messages:   4,
			wantAtOnce: 4,
		},
		{
			name:       "ceiling above demand serves every message at once",
			policy:     RPCPoolPolicy{MaxWorkers: 8, QueueDepth: 8},
			messages:   6,
			wantAtOnce: 6,
		},
		{
			name:       "default policy overlaps to its ceiling",
			policy:     RPCPoolPolicy{},
			messages:   defaultRPCMaxWorkers,
			wantAtOnce: defaultRPCMaxWorkers,
		},
		{
			name:       "a single worker reproduces the inline serial behavior",
			policy:     RPCPoolPolicy{MaxWorkers: 1, QueueDepth: 4},
			messages:   4,
			wantAtOnce: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newRPCPool(tt.policy)
			probe := newOverlapProbe(tt.messages)

			senders := submitAll(pool, tt.messages, probe.handle)
			probe.awaitArrivals(t, tt.wantAtOnce)
			close(probe.release)

			waitGroupWithin(t, senders, "submits to return")
			drainWithin(t, pool)

			if got := int(probe.peak.Load()); got != tt.wantAtOnce {
				t.Fatalf("peak concurrent handlers = %d, want %d", got, tt.wantAtOnce)
			}
			if got := int(probe.completed.Load()); got != tt.messages {
				t.Fatalf("completed handlers = %d, want %d", got, tt.messages)
			}
		})
	}
}

func TestRPCPoolBoundsOutstandingMessages(t *testing.T) {
	tests := []struct {
		name        string
		policy      RPCPoolPolicy
		messages    int
		wantAccept  int
		wantAtOnce  int
		wantBlocked int
	}{
		{
			name:        "two workers one queued four blocked",
			policy:      RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 1},
			messages:    7,
			wantAccept:  3,
			wantAtOnce:  2,
			wantBlocked: 4,
		},
		{
			name:        "unbuffered pool bounds on workers alone",
			policy:      RPCPoolPolicy{MaxWorkers: 3, QueueDepth: -1},
			messages:    6,
			wantAccept:  3,
			wantAtOnce:  3,
			wantBlocked: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newRPCPool(tt.policy)
			probe := newOverlapProbe(tt.messages)

			var accepted atomic.Int64
			callback := pool.callback(probe.handle)
			var senders sync.WaitGroup
			for i := 0; i < tt.messages; i++ {
				senders.Add(1)
				go func() {
					defer senders.Done()
					callback(&nats.Msg{Subject: "bagel.rpc.test"})
					accepted.Add(1)
				}()
			}

			probe.awaitArrivals(t, tt.wantAtOnce)
			waitFor(t, func() bool {
				return int(accepted.Load()) == tt.wantAccept
			}, "timed out waiting for the admitted messages to settle")

			if got := tt.messages - int(accepted.Load()); got != tt.wantBlocked {
				t.Fatalf("blocked senders = %d, want %d", got, tt.wantBlocked)
			}
			if got := int(probe.running.Load()); got != tt.wantAtOnce {
				t.Fatalf("running handlers = %d, want %d", got, tt.wantAtOnce)
			}

			close(probe.release)
			waitGroupWithin(t, &senders, "blocked submits to be released")
			drainWithin(t, pool)

			if got := int(probe.peak.Load()); got != tt.wantAtOnce {
				t.Fatalf("peak concurrent handlers = %d, want %d", got, tt.wantAtOnce)
			}
			if got := int(probe.completed.Load()); got != tt.messages {
				t.Fatalf("completed handlers = %d, want %d, so a message was lost", got, tt.messages)
			}
		})
	}
}

func TestRPCPoolDrainWaitsForInFlightHandlers(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 4, QueueDepth: 4})
	probe := newOverlapProbe(4)

	senders := submitAll(pool, 4, probe.handle)
	probe.awaitArrivals(t, 4)

	drained := drainAsync(pool)
	requireStillDraining(t, drained)

	close(probe.release)
	waitGroupWithin(t, senders, "submits to return")
	requireDrained(t, drained)

	if got := int(probe.completed.Load()); got != 4 {
		t.Fatalf("completed handlers = %d, want 4", got)
	}
	if inflight, workers := pool.stats(); inflight != 0 || workers != 0 {
		t.Fatalf("after Drain: inflight = %d, workers = %d, want 0 and 0", inflight, workers)
	}
}

func TestRPCPoolDrainHonoursDeadline(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 2})
	probe := newOverlapProbe(2)

	senders := submitAll(pool, 2, probe.handle)
	probe.awaitArrivals(t, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := pool.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Drain() = %v, want context.DeadlineExceeded", err)
	}

	close(probe.release)
	waitGroupWithin(t, senders, "submits to return")
	drainWithin(t, pool)
}

func TestRPCPoolRejectsSubmitsAfterDrain(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 2})

	var ran atomic.Int64
	callback := pool.callback(func(*nats.Msg) { ran.Add(1) })
	callback(&nats.Msg{Subject: "bagel.rpc.test"})
	waitFor(t, func() bool { return ran.Load() == 1 }, "the first message was never handled")

	drainWithin(t, pool)

	for i := 0; i < 8; i++ {
		callback(&nats.Msg{Subject: "bagel.rpc.test"})
	}
	if got := ran.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1: a post-drain delivery was executed", got)
	}
}

func TestRPCPoolRetiresIdleWorkers(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 6, QueueDepth: 6, IdleTimeout: 10 * time.Millisecond})
	probe := newOverlapProbe(6)

	senders := submitAll(pool, 6, probe.handle)
	probe.awaitArrivals(t, 6)
	if _, workers := pool.stats(); workers != 6 {
		t.Fatalf("workers at saturation = %d, want 6", workers)
	}

	close(probe.release)
	waitGroupWithin(t, senders, "submits to return")

	waitFor(t, func() bool {
		_, workers := pool.stats()
		return workers == 1
	}, "idle workers never retired to the floor")
	drainWithin(t, pool)
}

func TestRPCPoolLeaksNoGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 8, QueueDepth: 8})
	probe := newOverlapProbe(16)
	senders := submitAll(pool, 16, probe.handle)
	probe.awaitArrivals(t, 8)
	close(probe.release)
	waitGroupWithin(t, senders, "submits to return")
	drainWithin(t, pool)

	if _, workers := pool.stats(); workers != 0 {
		t.Fatalf("live workers after Drain = %d, want 0", workers)
	}
	waitFor(t, func() bool {
		return runtime.NumGoroutine() <= baseline
	}, "goroutines never returned to the baseline after Drain")
}

func TestRPCPoolPolicyNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   RPCPoolPolicy
		want RPCPoolPolicy
	}{
		{
			name: "zero value takes the fleet defaults",
			in:   RPCPoolPolicy{},
			want: RPCPoolPolicy{
				MinWorkers:  defaultRPCMinWorkers,
				MaxWorkers:  defaultRPCMaxWorkers,
				QueueDepth:  defaultRPCMaxWorkers,
				IdleTimeout: defaultRPCIdleTimeout,
			},
		},
		{
			name: "ceiling below the floor is raised to it",
			in:   RPCPoolPolicy{MinWorkers: 4, MaxWorkers: 2, QueueDepth: 3, IdleTimeout: time.Second},
			want: RPCPoolPolicy{MinWorkers: 4, MaxWorkers: 4, QueueDepth: 3, IdleTimeout: time.Second},
		},
		{
			name: "negative queue depth means an unbuffered handoff",
			in:   RPCPoolPolicy{MinWorkers: 2, MaxWorkers: 5, QueueDepth: -8, IdleTimeout: time.Minute},
			want: RPCPoolPolicy{MinWorkers: 2, MaxWorkers: 5, QueueDepth: 0, IdleTimeout: time.Minute},
		},
		{
			name: "negative floor and idle timeout fall back",
			in:   RPCPoolPolicy{MinWorkers: -1, MaxWorkers: 3, QueueDepth: 2, IdleTimeout: -time.Second},
			want: RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 3, QueueDepth: 2, IdleTimeout: defaultRPCIdleTimeout},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.normalized(); got != tt.want {
				t.Fatalf("normalized() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDrainRPCHandlersCoversRegisteredPools(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 2})
	registerRPCPool(pool)

	var ran atomic.Int64
	callback := pool.callback(func(*nats.Msg) { ran.Add(1) })
	callback(&nats.Msg{Subject: "bagel.rpc.test"})
	waitFor(t, func() bool { return ran.Load() == 1 }, "the message was never handled")

	ctx, cancel := context.WithTimeout(context.Background(), rpcPoolTestTimeout)
	defer cancel()
	if err := DrainRPCHandlers(ctx); err != nil {
		t.Fatalf("DrainRPCHandlers() = %v", err)
	}
	if _, workers := pool.stats(); workers != 0 {
		t.Fatalf("live workers after DrainRPCHandlers = %d, want 0", workers)
	}
	if err := DrainRPCHandlers(ctx); err != nil {
		t.Fatalf("second DrainRPCHandlers() = %v", err)
	}
}
