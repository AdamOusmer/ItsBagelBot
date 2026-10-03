// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	for i := range n {
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
	for range n {
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
		require.NoError(t, err)
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
	require.NoError(t, pool.Drain(ctx))
}

func TestRPCPoolRunsHandlersConcurrently(t *testing.T) {
	for _, tc := range []struct {
		name       string
		policy     RPCPoolPolicy
		messages   int
		wantAtOnce int
	}{
		{"four workers overlap four messages", RPCPoolPolicy{MaxWorkers: 4, QueueDepth: 4}, 4, 4},
		{"ceiling above demand serves every message at once", RPCPoolPolicy{MaxWorkers: 8, QueueDepth: 8}, 6, 6},
		{"default policy overlaps to its ceiling", RPCPoolPolicy{}, defaultRPCMaxWorkers, defaultRPCMaxWorkers},
		{"a single worker reproduces the inline serial behavior", RPCPoolPolicy{MaxWorkers: 1, QueueDepth: 4}, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newRPCPool(tc.policy)
			probe := newOverlapProbe(tc.messages)

			senders := submitAll(pool, tc.messages, probe.handle)
			probe.awaitArrivals(t, tc.wantAtOnce)
			close(probe.release)
			waitGroupWithin(t, senders, "submits to return")
			drainWithin(t, pool)

			assert.Equal(t, [2]int64{int64(tc.wantAtOnce), int64(tc.messages)}, [2]int64{probe.peak.Load(), probe.completed.Load()},
				"peak concurrent handlers and completed handlers")
		})
	}
}

func TestRPCPoolBoundsOutstandingMessages(t *testing.T) {
	for _, tc := range []struct {
		name        string
		policy      RPCPoolPolicy
		messages    int
		wantAccept  int
		wantAtOnce  int
		wantBlocked int
	}{
		{"two workers one queued four blocked", RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 1}, 7, 3, 2, 4},
		{"unbuffered pool bounds on workers alone", RPCPoolPolicy{MaxWorkers: 3, QueueDepth: -1}, 6, 3, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newRPCPool(tc.policy)
			probe := newOverlapProbe(tc.messages)
			var accepted atomic.Int64
			callback := pool.callback(probe.handle)
			var senders sync.WaitGroup
			for range tc.messages {
				senders.Add(1)
				go func() {
					defer senders.Done()
					callback(&nats.Msg{Subject: "bagel.rpc.test"})
					accepted.Add(1)
				}()
			}

			probe.awaitArrivals(t, tc.wantAtOnce)
			waitFor(t, func() bool { return int(accepted.Load()) == tc.wantAccept }, "timed out waiting for the admitted messages to settle")

			assert.Equal(t, tc.wantBlocked, tc.messages-int(accepted.Load()), "blocked senders")
			assert.EqualValues(t, tc.wantAtOnce, probe.running.Load(), "running handlers")
			close(probe.release)
			waitGroupWithin(t, &senders, "blocked submits to be released")
			drainWithin(t, pool)
			assert.Equal(t, [2]int64{int64(tc.wantAtOnce), int64(tc.messages)}, [2]int64{probe.peak.Load(), probe.completed.Load()},
				"peak concurrent handlers and completed handlers, so no message was lost")
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

	inflight, workers := pool.stats()
	assert.EqualValues(t, 4, probe.completed.Load())
	assert.Equal(t, [2]int{0, 0}, [2]int{inflight, workers}, "inflight and workers after Drain")
}

func TestRPCPoolDrainHonoursDeadline(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MaxWorkers: 2, QueueDepth: 2})
	probe := newOverlapProbe(2)
	senders := submitAll(pool, 2, probe.handle)
	probe.awaitArrivals(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err := pool.Drain(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
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

	for range 8 {
		callback(&nats.Msg{Subject: "bagel.rpc.test"})
	}

	assert.EqualValues(t, 1, ran.Load(), "a post-drain delivery was executed")
}

func TestRPCPoolRetiresIdleWorkers(t *testing.T) {
	pool := newRPCPool(RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 6, QueueDepth: 6, IdleTimeout: 10 * time.Millisecond})
	probe := newOverlapProbe(6)
	senders := submitAll(pool, 6, probe.handle)
	probe.awaitArrivals(t, 6)
	_, saturated := pool.stats()

	close(probe.release)
	waitGroupWithin(t, senders, "submits to return")

	assert.Equal(t, 6, saturated, "workers at saturation")
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

	_, workers := pool.stats()

	assert.Zero(t, workers, "live workers after Drain")
	waitFor(t, func() bool { return runtime.NumGoroutine() <= baseline }, "goroutines never returned to the baseline after Drain")
}

func TestRPCPoolPolicyNormalized(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   RPCPoolPolicy
		want RPCPoolPolicy
	}{
		{
			"zero value takes the fleet defaults",
			RPCPoolPolicy{},
			RPCPoolPolicy{MinWorkers: defaultRPCMinWorkers, MaxWorkers: defaultRPCMaxWorkers, QueueDepth: defaultRPCMaxWorkers, IdleTimeout: defaultRPCIdleTimeout},
		},
		{
			"ceiling below the floor is raised to it",
			RPCPoolPolicy{MinWorkers: 4, MaxWorkers: 2, QueueDepth: 3, IdleTimeout: time.Second},
			RPCPoolPolicy{MinWorkers: 4, MaxWorkers: 4, QueueDepth: 3, IdleTimeout: time.Second},
		},
		{
			"negative queue depth means an unbuffered handoff",
			RPCPoolPolicy{MinWorkers: 2, MaxWorkers: 5, QueueDepth: -8, IdleTimeout: time.Minute},
			RPCPoolPolicy{MinWorkers: 2, MaxWorkers: 5, QueueDepth: 0, IdleTimeout: time.Minute},
		},
		{
			"negative floor and idle timeout fall back",
			RPCPoolPolicy{MinWorkers: -1, MaxWorkers: 3, QueueDepth: 2, IdleTimeout: -time.Second},
			RPCPoolPolicy{MinWorkers: 1, MaxWorkers: 3, QueueDepth: 2, IdleTimeout: defaultRPCIdleTimeout},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.normalized())
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

	require.NoError(t, DrainRPCHandlers(ctx))
	_, workers := pool.stats()

	assert.Zero(t, workers, "live workers after DrainRPCHandlers")
	assert.NoError(t, DrainRPCHandlers(ctx), "a second drain must be a no-op")
}
