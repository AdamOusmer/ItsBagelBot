// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRoutinePoolLaneLimitReserve(t *testing.T) {
	p := newRoutinePool([]int{25, 0}, 4)

	if got := p.laneLimit(0); got != 4 {
		t.Fatalf("premium limit = %d, want 4 (may use whole pool)", got)
	}
	if got := p.laneLimit(1); got != 3 {
		t.Fatalf("standard limit = %d, want 3 (pool minus premium reserve)", got)
	}
}

func TestRoutinePoolReserveSurvivesSmallPool(t *testing.T) {
	cases := []struct {
		capacity     int
		wantPremium  int
		wantStandard int
	}{
		{1, 1, 1},
		{2, 2, 1},
		{3, 3, 2},
		{4, 4, 3},
		{8, 8, 6},
	}
	for _, tc := range cases {
		p := newRoutinePool([]int{25, 0}, tc.capacity)
		if got := p.laneLimit(0); got != tc.wantPremium {
			t.Errorf("cap %d: premium limit = %d, want %d", tc.capacity, got, tc.wantPremium)
		}
		if got := p.laneLimit(1); got != tc.wantStandard {
			t.Errorf("cap %d: standard limit = %d, want %d", tc.capacity, got, tc.wantStandard)
		}
		if reserved := tc.capacity - tc.wantStandard; tc.capacity >= 2 && reserved < 1 {
			t.Errorf("cap %d: premium reserved %d slots, want >= 1", tc.capacity, reserved)
		}
	}
}

func TestRoutinePoolStandardCannotTakeReservedSlots(t *testing.T) {
	p := newRoutinePool([]int{25, 0}, 4)

	for i := 0; i < 3; i++ {
		if !p.acquire(1) {
			t.Fatalf("standard acquire %d should succeed", i)
		}
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

	if !p.acquire(0) {
		t.Fatal("premium acquire should succeed into its reserved slot")
	}

	p.release(1)
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("standard acquire did not unblock after release")
	}
}

func newTestFleet(capacity, max int, handle func(*Message) error) (*workerPool, *routinePool, *sync.WaitGroup) {
	gate := newRoutinePool([]int{0}, capacity)
	dispatched := &sync.WaitGroup{}
	procs := newConsumeLanes(nil, []WeightedLane{{Subject: "test.lane", Handle: handle}}, zap.NewNop())
	return newWorkerPool(procs, gate, dispatched, max), gate, dispatched
}

func dispatchOne(t *testing.T, w *workerPool, gate *routinePool, dispatched *sync.WaitGroup) {
	t.Helper()
	if !gate.acquire(0) {
		t.Fatal("gate refused an acquire on an open pool")
	}
	dispatched.Add(1)
	w.work <- dispatch{msg: NewMessage("id", nil), lane: 0}
}

func waitForWorkers(t *testing.T, w *workerPool, want int) {
	t.Helper()
	waitFor(t, func() bool { return w.liveWorkers() == want }, fmt.Sprintf("live workers never reached %d", want))
}

func TestWorkerPoolResizeFollowsCapacity(t *testing.T) {
	w, _, dispatched := newTestFleet(4, 4, func(*Message) error { return nil })

	for _, want := range []int{2, 3, 4, 2, 1} {
		w.resize(want)
		waitForWorkers(t, w, want)
	}

	w.stop()
	dispatched.Wait()
	waitForWorkers(t, w, 0)
}

func TestWorkerPoolShrinkFinishesCurrentMessage(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var finished int64

	w, gate, dispatched := newTestFleet(2, 2, func(*Message) error {
		entered <- struct{}{}
		<-release
		atomic.AddInt64(&finished, 1)
		return nil
	})
	w.resize(2)

	dispatchOne(t, w, gate, dispatched)
	dispatchOne(t, w, gate, dispatched)
	<-entered
	<-entered

	w.resize(1)

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&finished); got != 0 {
		t.Fatalf("finished = %d, want 0 (handlers are still blocked)", got)
	}
	if got := w.liveWorkers(); got != 2 {
		t.Fatalf("live workers = %d mid-message, want 2 (retirement must wait for the handler)", got)
	}

	close(release)
	waitForWorkers(t, w, 1)
	waitFor(t, func() bool { return atomic.LoadInt64(&finished) == 2 },
		"finished never reached 2 (both in-flight messages must run to completion)")

	w.stop()
	dispatched.Wait()
	waitForWorkers(t, w, 0)
}

func TestWorkerPoolGrowCancelsPendingRetirement(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})

	w, gate, dispatched := newTestFleet(2, 2, func(*Message) error {
		entered <- struct{}{}
		<-release
		return nil
	})
	w.resize(2)

	dispatchOne(t, w, gate, dispatched)
	dispatchOne(t, w, gate, dispatched)
	<-entered
	<-entered

	w.resize(1)
	w.resize(2)

	if got := w.liveWorkers(); got != 2 {
		t.Fatalf("live workers = %d after shrink+grow, want 2 (no spawn, no churn)", got)
	}

	close(release)
	time.Sleep(50 * time.Millisecond)
	if got := w.liveWorkers(); got != 2 {
		t.Fatalf("live workers = %d, want 2 (a cancelled retirement must not fire later)", got)
	}

	w.stop()
	dispatched.Wait()
	waitForWorkers(t, w, 0)
}

func TestWorkerPoolStopDrainsQueuedDispatches(t *testing.T) {
	var ran int64
	w, gate, dispatched := newTestFleet(4, 4, func(*Message) error {
		atomic.AddInt64(&ran, 1)
		return nil
	})
	w.resize(1)

	for i := 0; i < 4; i++ {
		dispatchOne(t, w, gate, dispatched)
	}

	w.stop()
	dispatched.Wait()

	if got := atomic.LoadInt64(&ran); got != 4 {
		t.Fatalf("ran = %d, want 4 (a queued dispatch was stranded by close)", got)
	}
	if inflight, _ := gate.stats(); inflight != 0 {
		t.Fatalf("gate inflight = %d after drain, want 0 (every slot released)", inflight)
	}
	waitForWorkers(t, w, 0)
}

func TestConsumerUnitSetRoutinesKeepsFleetAtLeastCapacity(t *testing.T) {
	w, gate, dispatched := newTestFleet(2, 6, func(*Message) error { return nil })
	w.resize(2)
	u := &consumerUnit{pool: gate, workers: w}

	for _, n := range []int{3, 4, 6, 5, 2, 1} {
		u.setRoutines(n)

		_, capacity := gate.stats()
		if capacity != n {
			t.Fatalf("capacity = %d, want %d", capacity, n)
		}
		if live := w.liveWorkers(); live < capacity {
			t.Fatalf("n=%d: %d live workers for %d slots; an admitted message would queue behind a running handler", n, live, capacity)
		}
		waitForWorkers(t, w, n)
	}

	w.stop()
	dispatched.Wait()
}

type fakeSub struct {
	ch chan *Message
}

func (f *fakeSub) Subscribe(ctx context.Context, _ string) (<-chan *Message, error) {
	out := make(chan *Message)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-f.ch:
				if !ok {
					return
				}
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

func (f *fakeSub) Close() error { return nil }

func TestConsumeWeightedProcessesAndScales(t *testing.T) {
	sub := &fakeSub{ch: make(chan *Message)}

	var processed int64
	handle := func(m *Message) error {
		atomic.AddInt64(&processed, 1)
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
	if err != nil {
		t.Fatalf("ConsumeWeighted: %v", err)
	}

	const total = 200
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total; i++ {
			sub.ch <- NewMessage("id", nil)
		}
	}()
	wg.Wait()

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt64(&processed) < total {
		select {
		case <-deadline:
			t.Fatalf("processed %d of %d", atomic.LoadInt64(&processed), total)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestConsumeWeightedDrainWaitsForInflight(t *testing.T) {
	sub := &fakeSub{ch: make(chan *Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	var finished int64
	handle := func(m *Message) error {
		close(started)
		<-release
		atomic.AddInt64(&finished, 1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	w, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Handle: handle, Reserve: 25},
		{Sub: sub, Subject: "standard", Handle: handle},
	}, ScalePolicy{MinRoutines: 1, MaxRoutines: 2, MaxConsumers: 1}, zap.NewNop())
	if err != nil {
		t.Fatalf("ConsumeWeighted: %v", err)
	}

	sub.ch <- NewMessage("id", nil)
	<-started

	cancel()

	drained := make(chan error, 1)
	go func() { drained <- w.Drain(context.Background()) }()

	select {
	case <-drained:
		t.Fatal("Drain returned before the in-flight handler finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-drained:
		if err != nil {
			t.Fatalf("Drain: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Drain did not return after the handler finished")
	}
	if got := atomic.LoadInt64(&finished); got != 1 {
		t.Fatalf("finished = %d, want 1", got)
	}
}

func TestConsumeWeightedDrainDeadline(t *testing.T) {
	sub := &fakeSub{ch: make(chan *Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	handle := func(m *Message) error {
		close(started)
		<-release
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Handle: handle},
	}, ScalePolicy{MinRoutines: 1, MaxRoutines: 1, MaxConsumers: 1}, zap.NewNop())
	if err != nil {
		t.Fatalf("ConsumeWeighted: %v", err)
	}

	sub.ch <- NewMessage("id", nil)
	<-started
	cancel()

	dctx, dcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer dcancel()
	if err := w.Drain(dctx); err == nil {
		t.Fatal("Drain returned nil, want a deadline error")
	}

	close(release)
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

func TestConsumeWeightedPremiumReserveSurvivesStandardFlood(t *testing.T) {
	sub := newLaneSub("premium", "standard")
	load := newHeldLoad()

	premiumRan := make(chan struct{}, 1)
	premium := func(*Message) error {
		premiumRan <- struct{}{}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Handle: premium, Reserve: 25},
		{Sub: sub, Subject: "standard", Handle: load.handle},
	}, ScalePolicy{MinRoutines: 4, MaxRoutines: 4, MaxConsumers: 1}, zap.NewNop())
	if err != nil {
		t.Fatalf("ConsumeWeighted: %v", err)
	}

	stopFlood := floodLane(sub.lanes["standard"], "standard")
	for i := 0; i < 3; i++ {
		<-load.entered
	}

	sub.lanes["premium"] <- NewMessage("premium", nil)
	awaitSignal(t, premiumRan, "premium was starved by the standard flood")

	if peak := load.highWater(); peak > 3 {
		t.Fatalf("standard held %d slots at once, want at most 3 (premium reserves one of four)", peak)
	}

	stopFlood()
	close(load.release)
	cancel()

	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	if err := w.Drain(dctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
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
	if l.now > l.peak {
		l.peak = l.now
	}
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
