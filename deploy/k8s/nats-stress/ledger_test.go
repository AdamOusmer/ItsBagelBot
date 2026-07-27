package main

import (
	"testing"
	"time"
)

func TestSeqLedgerSeedsOnTheFirstSequence(t *testing.T) {
	// The flow consumer opens at DeliverNew, so everything published before it
	// bound is legitimately absent. Charging it as a gap would report a loss the
	// lane never had.
	ledger := newSeqLedger()
	ledger.observe("p#0", 5_000_000)
	got := ledger.snapshot()
	if got.Delivered != 1 || got.Gaps != 0 || got.Regressions != 0 {
		t.Fatalf("first observation must only seed the cursor: %+v", got)
	}
}

func TestSeqLedgerSeparatesGapsFromRegressions(t *testing.T) {
	ledger := newSeqLedger()
	for _, seq := range []uint64{1, 2, 5, 5, 4, 6} {
		ledger.observe("p#0", seq)
	}
	got := ledger.snapshot()
	if got.Delivered != 6 {
		t.Fatalf("delivered = %d, want 6", got.Delivered)
	}
	if got.Gaps != 2 { // 3 and 4 never arrived
		t.Fatalf("gaps = %d, want 2", got.Gaps)
	}
	if got.Regressions != 2 { // the repeated 5 and the late 4
		t.Fatalf("regressions = %d, want 2", got.Regressions)
	}
}

func TestSeqLedgerKeepsLanesIndependent(t *testing.T) {
	// Two publisher replicas interleave on one stream; a shared cursor would
	// report every interleave as a gap.
	ledger := newSeqLedger()
	ledger.observe("a#0", 1)
	ledger.observe("b#0", 900)
	ledger.observe("a#0", 2)
	ledger.observe("b#0", 901)
	got := ledger.snapshot()
	if got.Gaps != 0 || got.Regressions != 0 {
		t.Fatalf("independent lanes must not interfere: %+v", got)
	}
	if got.Lanes != 2 {
		t.Fatalf("lanes = %d, want 2", got.Lanes)
	}
}

func TestClassifyMatrix(t *testing.T) {
	cases := []struct {
		name  string
		entry guardEntry
		want  guardStats
	}{
		{
			name:  "treatment duplicate caught",
			entry: guardEntry{class: classTreatment, applies: 1, skips: 1},
			want:  guardStats{Events: 1, DupsCaught: 1},
		},
		{
			name:  "treatment duplicate applied twice",
			entry: guardEntry{class: classTreatment, applies: 2},
			want:  guardStats{Events: 1, DupsMissed: 1},
		},
		{
			name:  "treatment whose second copy never arrived",
			entry: guardEntry{class: classTreatment, applies: 1},
			want:  guardStats{Events: 1, DupsUnobserved: 1},
		},
		{
			name:  "control applied once is the healthy case",
			entry: guardEntry{class: classControl, applies: 1},
			want:  guardStats{Events: 1},
		},
		{
			name:  "control redelivery caught is not an injected duplicate",
			entry: guardEntry{class: classControl, applies: 1, skips: 2},
			want:  guardStats{Events: 1, ControlCaught: 2},
		},
		{
			name:  "control applied twice is a real miss",
			entry: guardEntry{class: classControl, applies: 3},
			want:  guardStats{Events: 1, ControlDoubleApplied: 2},
		},
		{
			name:  "suppressed without ever applying is a false positive",
			entry: guardEntry{class: classControl, skips: 1},
			want:  guardStats{Events: 1, FalsePositives: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.entry); got != tc.want {
				t.Fatalf("classify(%+v) = %+v, want %+v", tc.entry, got, tc.want)
			}
		})
	}
}

func TestGuardLedgerClassifiesOnlyAfterRotation(t *testing.T) {
	// Classifying at arrival would race the guard: the second copy can learn it
	// is a duplicate before the first copy has recorded that it applied.
	now := time.Unix(0, 0)
	ledger := newGuardLedger(time.Minute, func() time.Time { return now })

	ledger.record("e1", classTreatment, false) // skip observed first
	ledger.record("e1", classTreatment, true)  // apply recorded after
	if got := ledger.snapshot(); got.Events != 0 || got.FalsePositives != 0 {
		t.Fatalf("nothing may be classified while the window is live: %+v", got)
	}

	got := ledger.flush()
	if got.DupsCaught != 1 || got.FalsePositives != 0 {
		t.Fatalf("out-of-order arrival must resolve to a caught duplicate: %+v", got)
	}
}

func TestGuardLedgerPromotesAcrossARotation(t *testing.T) {
	now := time.Unix(0, 0)
	ledger := newGuardLedger(time.Minute, func() time.Time { return now })
	ledger.record("e1", classTreatment, true)

	now = now.Add(2 * time.Minute) // one rotation: e1 moves to the previous generation
	ledger.record("e1", classTreatment, false)

	got := ledger.flush()
	if got.DupsCaught != 1 {
		t.Fatalf("an event straddling a rotation must stay one event: %+v", got)
	}
	if got.FalsePositives != 0 {
		t.Fatalf("promotion failed and the delayed copy was charged as a false positive: %+v", got)
	}
}

func TestGuardLedgerFlushIsIdempotent(t *testing.T) {
	ledger := newGuardLedger(time.Minute, nil)
	ledger.record("e1", classControl, true)
	first := ledger.flush()
	second := ledger.flush()
	if first != second {
		t.Fatalf("flushing twice must not double-count: %+v then %+v", first, second)
	}
}

func TestSaturateStopsAtTheCounterCeiling(t *testing.T) {
	if got := saturate(^uint16(0)); got != ^uint16(0) {
		t.Fatalf("saturate wrapped: %d", got)
	}
	if got := saturate(3); got != 4 {
		t.Fatalf("saturate(3) = %d, want 4", got)
	}
}

func TestGuardMetricsCount(t *testing.T) {
	metrics := &guardMetrics{}
	metrics.Duplicate()
	metrics.Duplicate()
	metrics.FailOpen()
	duplicates, failOpen := metrics.snapshot()
	if duplicates != 2 || failOpen != 1 {
		t.Fatalf("snapshot = (%d, %d), want (2, 1)", duplicates, failOpen)
	}
}
