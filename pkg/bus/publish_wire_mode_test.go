// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"testing"
	"time"
)

// NATS_PUBLISH_WIRE is set in no manifest under deploy/, so this default is the
// whole fleet's behaviour: every Go publisher takes it the moment its image
// rolls. It must therefore be the wire that is already running. A batching
// default would flip sesame, outgress, users, projector and the data services in
// one deploy, and would spend the same per-stream budget of 50 open batches that
// the Elixir ingress fleet sizes itself against.
func TestPublishWireModeDefaultsToSingle(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "")
	if publishWireMode() != wireSingle {
		t.Fatal("unset NATS_PUBLISH_WIRE must select the per-message PubAck wire")
	}
}

// A typo must degrade, not escalate. Both batching wires have a wider blast
// radius than the default — an ambiguous atomic outcome costs a whole cohort
// where the single wire costs one message — so an unparseable value resolves to
// the smallest one rather than to the configured default.
func TestPublishWireModeFallsBackToTheSmallestBlastRadius(t *testing.T) {
	for _, value := range []string{"nonsense", "Atomic", "ATOMIC", "atomic ", "1"} {
		t.Setenv("NATS_PUBLISH_WIRE", value)
		if got := publishWireMode(); got != wireSingle {
			t.Errorf("NATS_PUBLISH_WIRE=%q selected %v; an unrecognised value must fall back to the per-message wire", value, got)
		}
	}
}

func TestPublishWireModeSelectsExplicitWires(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	if publishWireMode() != wireAtomic {
		t.Fatal("NATS_PUBLISH_WIRE=atomic must select the ADR-050 batch wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "single")
	if publishWireMode() != wireSingle {
		t.Fatal("NATS_PUBLISH_WIRE=single must select the per-message PubAck wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "fast")
	if publishWireMode() != wireFast {
		t.Fatal("NATS_PUBLISH_WIRE=fast must select the Fast-Ingest wire")
	}
}

func TestCohortWireBypassesBatchingForLoneMessage(t *testing.T) {
	if got := cohortWire(wireFast, 1); got != wireSingle {
		t.Fatalf("one-message fast cohort = %v, want the plain async wire", got)
	}
	if got := cohortWire(wireAtomic, 1); got != wireSingle {
		t.Fatalf("one-message atomic cohort = %v, want the plain async wire", got)
	}
	if got := cohortWire(wireFast, 2); got != wireFast {
		t.Fatalf("fast cohort = %v, want the Fast-Ingest wire", got)
	}
	if got := cohortWire(wireAtomic, 2); got != wireAtomic {
		t.Fatalf("atomic cohort = %v, want the ADR-050 wire", got)
	}
	if got := cohortWire(wireSingle, 128); got != wireSingle {
		t.Fatalf("single-wire cohort = %v, want the plain async wire", got)
	}
}

func TestFastPublishSettingsAreBounded(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "0")
	t.Setenv("NATS_FAST_PUBLISH_FLOW", "999999")
	t.Setenv("NATS_FAST_PUBLISH_OUTSTANDING_ACKS", "-1")

	if got := publishBatchSize(wireFast); got != 1 {
		t.Fatalf("fast batch size = %d, want lower bound 1", got)
	}
	if got := fastPublishFlow(1000, 8); got != 124 {
		t.Fatalf("fast flow = %d, want useful cohort ceiling 124", got)
	}
	if got := fastPublishOutstanding(); got != 1 {
		t.Fatalf("fast outstanding = %d, want lower bound 1", got)
	}
	if got := publishBatchSize(wireSingle); got != defaultPublishBatchSize {
		t.Fatalf("single batch size = %d, want %d", got, defaultPublishBatchSize)
	}
}

func TestFastPublishDefaultsToLongSession(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "")
	if got := publishBatchSize(wireFast); got != 8192 {
		t.Fatalf("default fast session size = %d, want 8192", got)
	}
}

// The broker enforces no maximum fast-batch size, so the clamp is a chosen
// blast-radius bound rather than a protocol ceiling; it still has to bind.
func TestFastPublishSessionSizeIsClampedToTheChosenBound(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "200000")
	if got := publishBatchSize(wireFast); got != fastSessionMax {
		t.Fatalf("fast session size = %d, want the chosen bound %d", got, fastSessionMax)
	}
}

func TestFastPublishBatchWaitIsBounded(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_WAIT", "500ms")
	if got := publishBatchWait(wireFast); got != 100*time.Millisecond {
		t.Fatalf("fast batch wait = %s, want upper bound 100ms", got)
	}
	if got := publishBatchWait(wireSingle); got != defaultPublishBatchWait {
		t.Fatalf("single batch wait = %s, want %s", got, defaultPublishBatchWait)
	}
}

// The cohort size is the only lever on the server's per-cohort RAFT proposal, so
// it is tunable; the clamp is the ADR-050 protocol range, not a preference.
func TestAtomicPublishCohortSizeIsBounded(t *testing.T) {
	t.Setenv("NATS_ATOMIC_PUBLISH_BATCH_SIZE", "")
	if got := publishBatchSize(wireAtomic); got != defaultAtomicPublishBatchSize {
		t.Fatalf("default atomic cohort = %d, want %d", got, defaultAtomicPublishBatchSize)
	}
	for value, want := range map[string]int{
		"1":       2,
		"0":       2,
		"-64":     2,
		"5000":    atomicBatchMax,
		"512":     512,
		"garbage": defaultAtomicPublishBatchSize,
	} {
		t.Setenv("NATS_ATOMIC_PUBLISH_BATCH_SIZE", value)
		if got := publishBatchSize(wireAtomic); got != want {
			t.Fatalf("NATS_ATOMIC_PUBLISH_BATCH_SIZE=%s gives cohort %d, want %d", value, got, want)
		}
	}
}

func TestAtomicPublishBatchWaitIsBounded(t *testing.T) {
	t.Setenv("NATS_ATOMIC_PUBLISH_BATCH_WAIT", "")
	if got := publishBatchWait(wireAtomic); got != defaultPublishBatchWait {
		t.Fatalf("default atomic batch wait = %s, want %s", got, defaultPublishBatchWait)
	}
	for value, want := range map[string]time.Duration{
		"100us": 500 * time.Microsecond,
		"0s":    500 * time.Microsecond,
		"1s":    20 * time.Millisecond,
		"8ms":   8 * time.Millisecond,
	} {
		t.Setenv("NATS_ATOMIC_PUBLISH_BATCH_WAIT", value)
		if got := publishBatchWait(wireAtomic); got != want {
			t.Fatalf("NATS_ATOMIC_PUBLISH_BATCH_WAIT=%s gives wait %s, want %s", value, got, want)
		}
	}
}

func TestAtomicPublishOverlapIsOnUnlessDisabled(t *testing.T) {
	t.Setenv("NATS_ATOMIC_PUBLISH_OVERLAP", "")
	if !atomicPublishOverlap() {
		t.Fatal("the commit overlap must be on by default")
	}
	t.Setenv("NATS_ATOMIC_PUBLISH_OVERLAP", "false")
	if atomicPublishOverlap() {
		t.Fatal("NATS_ATOMIC_PUBLISH_OVERLAP=false must keep the commit on the worker")
	}
}
