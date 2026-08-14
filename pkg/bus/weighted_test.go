package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestRoutinePoolLaneLimitReserve(t *testing.T) {
	// premium (lane 0) reserves 25%; standard (lane 1) reserves nothing.
	p := newRoutinePool([]int{25, 0}, 4)

	if got := p.laneLimit(0); got != 4 {
		t.Fatalf("premium limit = %d, want 4 (may use whole pool)", got)
	}
	if got := p.laneLimit(1); got != 3 {
		t.Fatalf("standard limit = %d, want 3 (pool minus premium reserve)", got)
	}
}

// TestRoutinePoolReserveSurvivesSmallPool pins the ceil rounding: a 25% premium
// reserve must hold at least one slot even in a 2- or 3-routine pool, where
// truncating capacity*reserve/100 down would round the reservation to zero and
// let a standard flood take the whole pool at raid onset (before the autoscaler
// grows the pool past capacity 4).
func TestRoutinePoolReserveSurvivesSmallPool(t *testing.T) {
	cases := []struct {
		capacity     int
		wantPremium  int // premium may use the whole pool
		wantStandard int // pool minus premium's rounded-up reserve
	}{
		{1, 1, 1}, // no reservation possible at one slot; neither lane starved
		{2, 2, 1}, // premium keeps 1 (old math gave standard 2, reserve 0)
		{3, 3, 2}, // premium keeps 1 (old math gave standard 3, reserve 0)
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

	// Standard fills its 3-slot allowance.
	for i := 0; i < 3; i++ {
		if !p.acquire(1) {
			t.Fatalf("standard acquire %d should succeed", i)
		}
	}

	// A 4th standard acquire must block (would eat premium's reserve).
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

	// Premium can still take the reserved slot even while standard is maxed.
	if !p.acquire(0) {
		t.Fatal("premium acquire should succeed into its reserved slot")
	}

	// Free a standard slot; the blocked standard acquire now proceeds.
	p.release(1)
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("standard acquire did not unblock after release")
	}
}

// newTestFleet wires an admission gate and a persistent worker fleet over one
// lane the way startUnit does, and returns the three pieces a reader touches.
// capacity is the gate's starting size, max the ceiling the fleet's channels are
// sized for (ScalePolicy.MaxRoutines).
func newTestFleet(capacity, max int, handle func(*Message) error) (*workerPool, *routinePool, *sync.WaitGroup) {
	gate := newRoutinePool([]int{0}, capacity)
	dispatched := &sync.WaitGroup{}
	procs := newConsumeLanes(nil, []WeightedLane{{Subject: "test.lane", Handle: handle}}, zap.NewNop())
	return newWorkerPool(procs, gate, dispatched, max), gate, dispatched
}

// dispatchOne performs exactly one reader step against the fleet: claim the
// lane's slot, count the dispatch for Drain, hand it over.
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.liveWorkers() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("live workers = %d, want %d", w.liveWorkers(), want)
}

// TestWorkerPoolResizeFollowsCapacity pins the cheap half of a resize: the fleet
// grows and shrinks by the delta the autoscaler asked for, and an idle worker
// retires promptly rather than lingering until the next message.
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

// TestWorkerPoolShrinkFinishesCurrentMessage pins the graceful half: a worker
// checks for its retirement token only between messages, so shrinking under load
// can never abandon a handler mid-flight or drop the ack it owes.
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
	<-entered // both workers are inside a handler

	w.resize(1) // the token has to wait: neither worker is between messages

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt64(&finished); got != 0 {
		t.Fatalf("finished = %d, want 0 (handlers are still blocked)", got)
	}
	if got := w.liveWorkers(); got != 2 {
		t.Fatalf("live workers = %d mid-message, want 2 (retirement must wait for the handler)", got)
	}

	close(release)
	waitForWorkers(t, w, 1)
	if got := atomic.LoadInt64(&finished); got != 2 {
		t.Fatalf("finished = %d, want 2 (both in-flight messages must run to completion)", got)
	}

	w.stop()
	dispatched.Wait()
	waitForWorkers(t, w, 0)
}

// TestWorkerPoolGrowCancelsPendingRetirement pins the no-churn rule: the
// autoscaler steps by ±1 every second, so a shrink immediately followed by a
// grow must take the retirement back rather than retire one goroutine and spawn
// another. Blocking the handlers holds the token in the buffer for the whole
// test, which is exactly the window a grow has to cancel it in.
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

	w.resize(1) // token queued, unreachable while both workers are busy
	w.resize(2) // must cancel it instead of spawning a third worker

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

// TestWorkerPoolStopDrainsQueuedDispatches pins the shutdown contract the
// WaitGroup discipline rests on: every dispatch a reader counted up is run by a
// worker even when it was still sitting in the work buffer when the channel
// closed. If close could strand a buffered dispatch, Drain would hang forever.
func TestWorkerPoolStopDrainsQueuedDispatches(t *testing.T) {
	var ran int64
	w, gate, dispatched := newTestFleet(4, 4, func(*Message) error {
		atomic.AddInt64(&ran, 1)
		return nil
	})
	w.resize(1) // one worker, so most of the dispatches queue in the buffer

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

// TestConsumerUnitSetRoutinesKeepsFleetAtLeastCapacity pins the ordering that
// makes a lane reserve worth a worker rather than a place in a queue: whichever
// direction the autoscaler moves, the fleet is never smaller than the number of
// slots the gate is handing out.
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

// fakeSub is a minimal Subscriber: every Subscribe returns the same
// shared channel so multiple consumers compete for one stream of messages.
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

// TestConsumeWeightedDrainWaitsForInflight pins the shutdown contract P1 depends
// on: after the consumer's context is cancelled (SIGTERM), Drain must not return
// until a handler that was already dispatched has run to completion, so main can
// flush reporters and close publishers without abandoning in-flight work.
func TestConsumeWeightedDrainWaitsForInflight(t *testing.T) {
	sub := &fakeSub{ch: make(chan *Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	var finished int64
	handle := func(m *Message) error {
		close(started) // exactly one message is sent, so handle runs once
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
	<-started // the handler is in-flight, blocked on release

	cancel() // stop pulling, as SIGTERM does

	drained := make(chan error, 1)
	go func() { drained <- w.Drain(context.Background()) }()

	// Drain must block while the handler is still running.
	select {
	case <-drained:
		t.Fatal("Drain returned before the in-flight handler finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(release) // let the handler complete

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

// TestConsumeWeightedDrainDeadline pins the other half: a handler that outlives
// the drain deadline does not hang shutdown forever; Drain returns the context
// error so main can log and exit, leaving the event for redelivery.
func TestConsumeWeightedDrainDeadline(t *testing.T) {
	sub := &fakeSub{ch: make(chan *Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	handle := func(m *Message) error {
		close(started)
		<-release // never released before the deadline
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

	close(release) // unblock the handler so the goroutine does not leak
}

// laneSub gives each subject its own stream, so a test can flood one lane
// without the other lane's reader stealing the messages (fakeSub shares one
// channel between every subscription and cannot express that).
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

// TestConsumeWeightedPremiumReserveSurvivesStandardFlood is the end-to-end form
// of the reserve invariant, and the one persistent workers had to preserve: the
// fleet is shared by every lane, so a standard flood that pinned every worker
// would starve premium just as effectively as one that took every slot. It
// cannot, because the fleet is never smaller than the gate's capacity and the
// gate caps standard below it — the reserved slot always has an idle worker.
func TestConsumeWeightedPremiumReserveSurvivesStandardFlood(t *testing.T) {
	sub := newLaneSub("premium", "standard")

	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var mu sync.Mutex
	var standardNow, standardPeak int
	standard := func(*Message) error {
		mu.Lock()
		standardNow++
		if standardNow > standardPeak {
			standardPeak = standardNow
		}
		mu.Unlock()
		entered <- struct{}{}
		<-release
		mu.Lock()
		standardNow--
		mu.Unlock()
		return nil
	}

	premiumRan := make(chan struct{}, 1)
	premium := func(*Message) error {
		premiumRan <- struct{}{}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Four routines with a 25% premium reserve: standard may hold 3, premium may
	// hold all 4, so the fourth slot and the fourth worker are premium's alone.
	w, err := ConsumeWeighted(ctx, nil, []WeightedLane{
		{Sub: sub, Subject: "premium", Handle: premium, Reserve: 25},
		{Sub: sub, Subject: "standard", Handle: standard},
	}, ScalePolicy{MinRoutines: 4, MaxRoutines: 4, MaxConsumers: 1}, zap.NewNop())
	if err != nil {
		t.Fatalf("ConsumeWeighted: %v", err)
	}

	flood := make(chan struct{})
	go func() {
		for {
			select {
			case <-flood:
				return
			case sub.lanes["standard"] <- NewMessage("standard", nil):
			}
		}
	}()

	for i := 0; i < 3; i++ {
		<-entered // standard is holding its whole allowance, and blocking in it
	}

	sub.lanes["premium"] <- NewMessage("premium", nil)
	select {
	case <-premiumRan:
	case <-time.After(2 * time.Second):
		t.Fatal("premium was starved by the standard flood")
	}

	mu.Lock()
	peak := standardPeak
	mu.Unlock()
	if peak > 3 {
		t.Fatalf("standard held %d slots at once, want at most 3 (premium reserves one of four)", peak)
	}

	close(flood)
	close(release)
	cancel()

	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	if err := w.Drain(dctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

// BenchmarkDispatchHandoff and BenchmarkDispatchGoroutinePerMessage measure the
// one thing this change replaced: what the serial reader loop pays to get a
// message onto another goroutine. That cost is the lane's dispatch ceiling —
// the loop runs once per message on a single goroutine per lane — so it is worth
// isolating from everything around it.
//
// The admission gate is deliberately outside both loops. It is untouched here,
// common to both shapes, and expensive enough per release (a cond.Broadcast that
// wakes every waiter) to bury the difference rather than show it; measured
// together the two shapes land within noise of each other and of the gate.
// BenchmarkDispatchWorkerPool is the whole path with the gate back in.
//
// Both keep the Drain bookkeeping, because that is per-message cost that did not
// change and it must not be attributed to either mechanism.
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

// BenchmarkDispatchGoroutinePerMessage is the standing control: the shape
// startReaders used before persistent workers, spawning one goroutine per
// message from inside the serial loop with the same message hand-off and the
// same Drain bookkeeping. It is kept because the two shapes cannot otherwise be
// measured against each other in one tree.
func BenchmarkDispatchGoroutinePerMessage(b *testing.B) {
	dispatched := &sync.WaitGroup{}
	msg := NewMessage("bench", nil)

	b.ReportAllocs()
	for b.Loop() {
		dispatched.Add(1)
		go func(m *Message) {
			defer dispatched.Done()
			_ = m // the old dispatch carried the message in and nothing else
		}(msg)
	}

	dispatched.Wait()
}

// BenchmarkDispatchWorkerPool measures the whole path end to end — reader step,
// admission gate, hand-off, worker, and consumeLane.process with a no-op handler
// and no New Relic application — so the dispatch mechanism can be read against
// the per-message work it feeds. Unlike the two above it allocates one Message
// per iteration, which no reader does: the subscriber owns that allocation.
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
