package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
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

// fakeSub is a minimal message.Subscriber: every Subscribe returns the same
// shared channel so multiple consumers compete for one stream of messages.
type fakeSub struct {
	ch chan *message.Message
}

func (f *fakeSub) Subscribe(ctx context.Context, _ string) (<-chan *message.Message, error) {
	out := make(chan *message.Message)
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
	sub := &fakeSub{ch: make(chan *message.Message)}

	var processed int64
	handle := func(m *message.Message) error {
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
			sub.ch <- message.NewMessage("id", nil)
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
	sub := &fakeSub{ch: make(chan *message.Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	var finished int64
	handle := func(m *message.Message) error {
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

	sub.ch <- message.NewMessage("id", nil)
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
	sub := &fakeSub{ch: make(chan *message.Message)}

	started := make(chan struct{})
	release := make(chan struct{})
	handle := func(m *message.Message) error {
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

	sub.ch <- message.NewMessage("id", nil)
	<-started
	cancel()

	dctx, dcancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer dcancel()
	if err := w.Drain(dctx); err == nil {
		t.Fatal("Drain returned nil, want a deadline error")
	}

	close(release) // unblock the handler so the goroutine does not leak
}
