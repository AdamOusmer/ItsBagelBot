package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPacerHoldsAnAbsoluteSchedule(t *testing.T) {
	// Absolute, not incremental: a slow patch must be caught up rather than
	// quietly lowering the offered rate, which is what makes the load open-loop.
	start := time.Now()
	pace := newPacer(start, 1000) // 1ms per message
	defer pace.stop()
	done := make(chan struct{})

	for i := 0; i < 20; i++ {
		if !pace.wait(done) {
			t.Fatal("pacer aborted without cancellation")
		}
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("20 messages at 1000/s took %v; the pacer is not batching short waits", elapsed)
	}
}

func TestPacerStopsOnCancellation(t *testing.T) {
	done := make(chan struct{})
	close(done)
	pace := newPacer(time.Now().Add(time.Hour), 1) // every send is far in the future
	defer pace.stop()
	if pace.wait(done) {
		t.Fatal("a cancelled run must not keep sending")
	}
}

func TestPacerReportsCancellationEvenWhenItWouldNotSleep(t *testing.T) {
	done := make(chan struct{})
	close(done)
	pace := newPacer(time.Now().Add(-time.Hour), 1000) // every send is already due
	defer pace.stop()
	if pace.wait(done) {
		t.Fatal("the no-sleep fast path must still honour cancellation")
	}
}

func TestClassifySplitsTreatmentControlAndPlain(t *testing.T) {
	lane := newLaneState("p0", 0)
	now := time.Now()
	counts := map[eventClass]int{}
	for i := 0; i < 20000; i++ {
		counts[lane.classify(0.01, time.Millisecond, now, "e")]++
	}
	// The control set must be the same size as the treatment set, or "zero false
	// positives" is measured over a population of zero.
	treatment, control := counts[classTreatment], counts[classControl]
	if treatment == 0 || control == 0 {
		t.Fatalf("both sampled classes must be populated: %v", counts)
	}
	ratio := float64(treatment) / float64(control)
	if ratio < 0.7 || ratio > 1.4 {
		t.Fatalf("treatment/control = %.2f, want roughly 1 (%v)", ratio, counts)
	}
	if counts[classPlain] < 19000 {
		t.Fatalf("the firehose must stay unsampled: %v", counts)
	}
}

func TestScheduleDupUsesBothVariants(t *testing.T) {
	lane := newLaneState("p0", 0)
	now := time.Now()
	for i := 0; i < 2000; i++ {
		lane.scheduleDup("e", time.Second, now)
	}
	if len(lane.immediate) == 0 || len(lane.delayed) == 0 {
		t.Fatalf("both duplicate variants must be produced: immediate=%d delayed=%d",
			len(lane.immediate), len(lane.delayed))
	}
}

func TestScheduleDupDropsRatherThanGrowingWithoutBound(t *testing.T) {
	lane := newLaneState("p0", 0)
	now := time.Now()
	for i := 0; i < pendingDupLimit+500; i++ {
		lane.scheduleDup("e", time.Hour, now) // nothing ever becomes due
	}
	if queued := len(lane.immediate) + len(lane.delayed); queued > pendingDupLimit {
		t.Fatalf("queued %d duplicates, over the %d bound", queued, pendingDupLimit)
	}
	if lane.dropped == 0 {
		t.Fatal("the overflow must be counted, not silently discarded")
	}
}

// The sample must never gate the offered load: a full pool refuses the work
// rather than making the caller wait for a slot.
func TestSamplePoolRefusesRatherThanBlocking(t *testing.T) {
	pool := newSamplePool(2)
	release := make(chan struct{})
	var ran atomic.Int64

	for i := 0; i < 2; i++ {
		if !pool.offer(func() { ran.Add(1); <-release }) {
			t.Fatalf("sample %d was refused a free slot", i)
		}
	}

	refused := make(chan bool, 1)
	go func() { refused <- pool.offer(func() { ran.Add(1) }) }()
	select {
	case accepted := <-refused:
		if accepted {
			t.Fatal("a full pool accepted a third sample")
		}
	case <-time.After(time.Second):
		t.Fatal("offer blocked on a full pool")
	}

	close(release)
	pool.wait()
	if ran.Load() != 2 {
		t.Fatalf("pool ran %d samples, want the two it accepted", ran.Load())
	}
}

// A slot freed by a finished sample has to come back, or the pool degrades to
// its first samplePoolSize samples for the whole run.
func TestSamplePoolReturnsSlots(t *testing.T) {
	pool := newSamplePool(1)
	for i := 0; i < 8; i++ {
		if !pool.offer(func() {}) {
			t.Fatalf("offer %d found no free slot after the previous one finished", i)
		}
		pool.wait()
	}
}

// wait is what makes a step's counters its own: a confirmed publish still in
// flight when the step closes would be charged to the next rate.
func TestSamplePoolWaitDrainsOutstandingSamples(t *testing.T) {
	pool := newSamplePool(4)
	var done atomic.Int64
	for i := 0; i < 4; i++ {
		pool.offer(func() {
			time.Sleep(10 * time.Millisecond)
			done.Add(1)
		})
	}

	pool.wait()
	if done.Load() != 4 {
		t.Fatalf("wait returned with %d/4 samples still in flight", done.Load())
	}
}

// The interval belongs to the flag, not to pool pressure: the counter advances
// on every send whether or not the sample is ultimately taken.
func TestSampleDueHoldsTheConfiguredInterval(t *testing.T) {
	state := &publishState{cfg: publisherConfig{SampleEvery: 4}}
	var due int
	for i := 0; i < 20; i++ {
		if state.sampleDue() {
			due++
		}
	}
	if due != 5 {
		t.Fatalf("20 sends at 1-in-4 produced %d samples, want 5", due)
	}

	off := &publishState{cfg: publisherConfig{SampleEvery: 0}}
	if off.sampleDue() {
		t.Fatal("sampling disabled must owe no samples")
	}
}

func TestSettleScoresEachPublishExactlyOnce(t *testing.T) {
	state := &publishState{}
	state.settle(nil)
	state.settle(errors.New("cohort failed"))
	if state.achieved.Load() != 1 || state.failures.Load() != 1 {
		t.Fatalf("achieved=%d failures=%d, want one of each",
			state.achieved.Load(), state.failures.Load())
	}
}

func TestNextDupPrefersImmediateAndHoldsDelayedUntilDue(t *testing.T) {
	lane := newLaneState("p0", 0)
	now := time.Now()
	lane.delayed = append(lane.delayed, pendingDup{id: "later", due: now.Add(time.Minute)})
	lane.immediate = append(lane.immediate, "now")

	if id, ok := lane.nextDup(now); !ok || id != "now" {
		t.Fatalf("back-to-back duplicate must go first, got (%q, %v)", id, ok)
	}
	if _, ok := lane.nextDup(now); ok {
		t.Fatal("a delayed duplicate must not be released before it is due")
	}
	if id, ok := lane.nextDup(now.Add(2 * time.Minute)); !ok || id != "later" {
		t.Fatalf("delayed duplicate never became due: (%q, %v)", id, ok)
	}
}
