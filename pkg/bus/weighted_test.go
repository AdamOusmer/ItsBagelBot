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

	err := ConsumeWeighted(ctx, nil, []WeightedLane{
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
