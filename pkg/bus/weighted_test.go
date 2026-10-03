// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRoutinePoolReserveSurvivesSmallPool(t *testing.T) {
	for _, tc := range []struct {
		name         string
		capacity     int
		wantPremium  int
		wantStandard int
	}{
		{"cap 1", 1, 1, 1},
		{"cap 2", 2, 2, 1},
		{"cap 3", 3, 3, 2},
		{"TestRoutinePoolLaneLimitReserve", 4, 4, 3},
		{"cap 8", 8, 8, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newRoutinePool([]int{25, 0}, tc.capacity)

			assert.Equal(t, tc.wantPremium, p.laneLimit(0), "premium may use the whole pool")
			assert.Equal(t, tc.wantStandard, p.laneLimit(1), "standard gets the pool minus the premium reserve")
		})
	}
}

func TestRoutinePoolStandardCannotTakeReservedSlots(t *testing.T) {
	p := newRoutinePool([]int{25, 0}, 4)
	for i := range 3 {
		require.True(t, p.acquire(1), "standard acquire %d should succeed", i)
	}
	blocked := make(chan struct{})
	go func() {
		p.acquire(1)
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("standard acquired a reserved slot")
	case <-time.After(50 * time.Millisecond):
	}

	assert.True(t, p.acquire(0), "premium acquire should succeed into its reserved slot")

	p.release(1)
	awaitSignal(t, blocked, "standard acquire did not unblock after release")
}

func newTestFleet(capacity, max int, handle func(*Message) error) (*workerPool, *routinePool, *sync.WaitGroup) {
	gate := newRoutinePool([]int{0}, capacity)
	dispatched := &sync.WaitGroup{}
	procs := newConsumeLanes(nil, []WeightedLane{{Subject: "test.lane", Handle: handle}}, zap.NewNop())
	return newWorkerPool(procs, gate, dispatched, max), gate, dispatched
}

func dispatchOne(t *testing.T, w *workerPool, gate *routinePool, dispatched *sync.WaitGroup) {
	t.Helper()
	require.True(t, gate.acquire(0), "gate refused an acquire on an open pool")
	dispatched.Add(1)
	w.work <- dispatch{msg: NewMessage("id", nil), lane: 0}
}

type laneSub struct {
	lanes map[string]chan *Message
}

func newLaneSub(subjects ...string) *laneSub {
	l := &laneSub{lanes: make(map[string]chan *Message, len(subjects))}
	for _, subject := range subjects {
		l.lanes[subject] = make(chan *Message)
	}
	return l
}

func newSharedLaneSub(subjects ...string) *laneSub {
	shared := make(chan *Message)
	l := &laneSub{lanes: make(map[string]chan *Message, len(subjects))}
	for _, subject := range subjects {
		l.lanes[subject] = shared
	}
	return l
}

func (l *laneSub) Subscribe(ctx context.Context, subject string) (<-chan *Message, error) {
	src := l.lanes[subject]
	out := make(chan *Message)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-src:
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (l *laneSub) Close() error { return nil }

func TestConsumeWeightedProcessesAndScales(t *testing.T) {
	sub := newSharedLaneSub("premium", "standard")
	var processed atomic.Int64
	handle := func(*Message) error {
		processed.Add(1)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Handle: handle, Reserve: 25},
		{Sub: sub, Subject: "standard", Handle: handle},
	}, ScalePolicy{
		MinRoutines:    2,
		MaxRoutines:    4,
		MaxConsumers:   2,
		ScaleUpAfter:   10 * time.Millisecond,
		ScaleDownAfter: 10 * time.Millisecond,
	}, zap.NewNop())
	require.NoError(t, err)

	const total = 200
	go func() {
		for range total {
			sub.lanes["premium"] <- NewMessage("id", nil)
		}
	}()

	waitFor(t, func() bool { return processed.Load() >= total }, "not every message was processed")
}

type weightedDrain struct {
	started  chan struct{}
	release  chan struct{}
	drained  chan error
	finished atomic.Int64
	weighted *Weighted
}

func startBlockedWeighted(t *testing.T, policy ScalePolicy, lanes func(handle func(*Message) error) []WeightedLane) *weightedDrain {
	t.Helper()
	d := &weightedDrain{started: make(chan struct{}), release: make(chan struct{}), drained: make(chan error, 1)}
	sub := newSharedLaneSub("premium", "standard")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	weighted, err := ConsumeWeighted(ctx, nil, withSub(sub, lanes(func(*Message) error {
		close(d.started)
		<-d.release
		d.finished.Add(1)
		return nil
	})), policy, zap.NewNop())
	require.NoError(t, err)
	d.weighted = weighted
	sub.lanes["premium"] <- NewMessage("id", nil)
	<-d.started
	cancel()
	return d
}

func withSub(sub Subscriber, lanes []WeightedLane) []WeightedLane {
	for i := range lanes {
		lanes[i].Sub = sub
	}
	return lanes
}

func TestConsumeWeightedDrainWaitsForInflight(t *testing.T) {
	d := startBlockedWeighted(t, ScalePolicy{MinRoutines: 1, MaxRoutines: 2, MaxConsumers: 1}, func(handle func(*Message) error) []WeightedLane {
		return []WeightedLane{{Subject: "premium", Handle: handle, Reserve: 25}, {Subject: "standard", Handle: handle}}
	})
	go func() { d.drained <- d.weighted.Drain(context.Background()) }()
	select {
	case <-d.drained:
		t.Fatal("Drain returned before the in-flight handler finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(d.release)

	select {
	case err := <-d.drained:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Drain did not return after the handler finished")
	}
	assert.EqualValues(t, 1, d.finished.Load())
}

func TestConsumeWeightedDrainDeadline(t *testing.T) {
	d := startBlockedWeighted(t, ScalePolicy{MinRoutines: 1, MaxRoutines: 1, MaxConsumers: 1}, func(handle func(*Message) error) []WeightedLane {
		return []WeightedLane{{Subject: "premium", Handle: handle}}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := d.weighted.Drain(ctx)

	assert.Error(t, err, "Drain must report the deadline while a handler is still running")
	close(d.release)
}

func TestConsumeWeightedPremiumReserveSurvivesStandardFlood(t *testing.T) {
	sub := newLaneSub("premium", "standard")
	load := newHeldLoad()
	premiumRan := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Reserve: 25, Handle: func(*Message) error {
			premiumRan <- struct{}{}
			return nil
		}},
		{Sub: sub, Subject: "standard", Handle: load.handle},
	}, ScalePolicy{MinRoutines: 4, MaxRoutines: 4, MaxConsumers: 1}, zap.NewNop())
	require.NoError(t, err)
	stopFlood := floodLane(sub.lanes["standard"], "standard")
	for range 3 {
		<-load.entered
	}

	sub.lanes["premium"] <- NewMessage("premium", nil)
	awaitSignal(t, premiumRan, "premium was starved by the standard flood")

	assert.LessOrEqual(t, load.highWater(), 3, "standard may hold at most 3 slots because premium reserves one of four")
	stopFlood()
	close(load.release)
	cancel()
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	assert.NoError(t, w.Drain(dctx))
}

type heldLoad struct {
	entered chan struct{}
	release chan struct{}

	mu   sync.Mutex
	now  int
	peak int
}

func newHeldLoad() *heldLoad {
	return &heldLoad{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (l *heldLoad) handle(*Message) error {
	l.enter()
	l.entered <- struct{}{}
	<-l.release
	l.leave()
	return nil
}

func (l *heldLoad) enter() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now++
	l.peak = max(l.peak, l.now)
}

func (l *heldLoad) leave() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now--
}

func (l *heldLoad) highWater() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak
}

func floodLane(lane chan<- *Message, id string) func() {
	flood := make(chan struct{})
	go func() {
		for {
			select {
			case <-flood:
				return
			case lane <- NewMessage(id, nil):
			}
		}
	}()
	return func() { close(flood) }
}

func BenchmarkDispatchHandoff(b *testing.B) {
	const routines = 8

	work := make(chan dispatch, routines)
	dispatched := &sync.WaitGroup{}

	var fleet sync.WaitGroup
	for i := 0; i < routines; i++ {
		fleet.Add(1)
		go func() {
			defer fleet.Done()
			for range work {
				dispatched.Done()
			}
		}()
	}

	msg := NewMessage("bench", nil)

	b.ReportAllocs()
	for b.Loop() {
		dispatched.Add(1)
		work <- dispatch{msg: msg, lane: 0}
	}

	close(work)
	fleet.Wait()
	dispatched.Wait()
}

func BenchmarkDispatchGoroutinePerMessage(b *testing.B) {
	dispatched := &sync.WaitGroup{}
	msg := NewMessage("bench", nil)

	b.ReportAllocs()
	for b.Loop() {
		dispatched.Add(1)
		go func(m *Message) {
			defer dispatched.Done()
			_ = m
		}(msg)
	}

	dispatched.Wait()
}

func BenchmarkDispatchWorkerPool(b *testing.B) {
	const routines = 8

	w, gate, dispatched := newTestFleet(routines, routines, func(*Message) error { return nil })
	w.resize(routines)

	b.ReportAllocs()
	for b.Loop() {
		msg := NewMessage("bench", nil)
		gate.acquire(0)
		dispatched.Add(1)
		w.work <- dispatch{msg: msg, lane: 0}
	}

	w.stop()
	dispatched.Wait()
}
