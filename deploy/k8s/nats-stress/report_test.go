package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func publisherLine(t *testing.T, s publisherSummary) string {
	t.Helper()
	s.envelopeHeader = envelopeHeader{Rig: rigName, Role: rolePublish, Kind: kindSummary}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func consumerLine(t *testing.T, s consumerSummary) string {
	t.Helper()
	s.envelopeHeader = envelopeHeader{Rig: rigName, Role: roleConsume, Kind: kindSummary}
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestReadSummariesSkipsEverythingButSummaries(t *testing.T) {
	// The merge is pointed straight at a kubectl logs capture, so it has to walk
	// past ticks, per-step lines, zap noise and truncated tails without failing.
	lines := strings.Join([]string{
		`{"rig":"nats-stress","role":"publish","kind":"tick"}`,
		`{"rig":"nats-stress","role":"publish","kind":"step"}`,
		`{"rig":"other","role":"publish","kind":"summary"}`,
		`not json at all`,
		publisherLine(t, publisherSummary{PublisherID: "p0"}),
		consumerLine(t, consumerSummary{ConsumerID: "c0"}),
	}, "\n")

	got, err := readSummaries(strings.NewReader(lines))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.publishers) != 1 || got.publishers[0].PublisherID != "p0" {
		t.Fatalf("publishers = %+v", got.publishers)
	}
	if len(got.consumers) != 1 || got.consumers[0].ConsumerID != "c0" {
		t.Fatalf("consumers = %+v", got.consumers)
	}
}

func TestMergeStepsSumsReplicasAndTakesTheSlowestWallClock(t *testing.T) {
	publishers := []publisherSummary{
		{Steps: []stepResult{{Index: 0, OfferedRate: 500, Offered: 5000, Achieved: 5000, ElapsedSec: 10}}},
		{Steps: []stepResult{{Index: 0, OfferedRate: 500, Offered: 5000, Achieved: 4000, ElapsedSec: 12, Failures: 3}}},
	}
	got := mergeSteps(publishers)
	if len(got) != 1 {
		t.Fatalf("steps = %+v", got)
	}
	step := got[0]
	if step.OfferedRate != 1000 || step.Achieved != 9000 || step.Failures != 3 {
		t.Fatalf("replica sums are wrong: %+v", step)
	}
	if step.ElapsedSec != 12 {
		t.Fatalf("elapsed = %v, want the slowest replica's 12 (they run concurrently)", step.ElapsedSec)
	}
}

func TestMergeStepsStopsAtTheFirstStepAReplicaSkipped(t *testing.T) {
	// A replica that breached one step earlier simply stopped ramping. Including
	// the step only the other replica ran would report a fleet rate nobody offered.
	publishers := []publisherSummary{
		{Steps: []stepResult{{Index: 0, ElapsedSec: 1}, {Index: 1, ElapsedSec: 1}}},
		{Steps: []stepResult{{Index: 0, ElapsedSec: 1}}},
	}
	if got := mergeSteps(publishers); len(got) != 1 {
		t.Fatalf("expected only the common step, got %d", len(got))
	}
}

func TestLagFlat(t *testing.T) {
	rising := []lagSample{{NumPending: 10}, {NumPending: 20}, {NumPending: 4000}, {NumPending: 9000}}
	if lagFlat(rising, 1.5, 100) {
		t.Fatal("a curve that grew 300x must not be flat")
	}
	steady := []lagSample{{NumPending: 900}, {NumPending: 1000}, {NumPending: 950}, {NumPending: 1010}}
	if !lagFlat(steady, 1.5, 100) {
		t.Fatal("a steady curve must be flat")
	}
	noisy := []lagSample{{NumPending: 1}, {NumPending: 2}, {NumPending: 8}, {NumPending: 9}}
	if !lagFlat(noisy, 1.5, 100) {
		t.Fatal("growth below the floor is scheduling noise, not lag")
	}
	if !lagFlat([]lagSample{{NumPending: 1}}, 1.5, 100) {
		t.Fatal("too few samples must not invent a failure")
	}
}

func healthyRun() summaries {
	step := stepResult{Index: 0, OfferedRate: 1000, Offered: 10000, Achieved: 10000, ElapsedSec: 10}
	soak := stepResult{Index: -1, OfferedRate: 850, Offered: 8500, Achieved: 8500, ElapsedSec: 10, Soak: true}
	return summaries{
		publishers: []publisherSummary{{Steps: []stepResult{step}, Soak: &soak}},
		consumers: []consumerSummary{{
			Seq:   seqCounts{Delivered: 10000},
			Guard: guardStats{DupsCaught: 100, Events: 200},
			Lag:   []lagSample{{NumPending: 100}, {NumPending: 110}, {NumPending: 105}, {NumPending: 120}},
		}},
	}
}

func defaultMergeConfig() mergeConfig {
	return mergeConfig{
		Detector:   ceilingDetector{Ratio: 0.97},
		StableFrac: 0.85, LagTolerance: 1.5, LagFloor: 5000, LatencyTol: 1.5,
	}
}

func TestMergePassesAHealthyRun(t *testing.T) {
	got := merge(healthyRun(), defaultMergeConfig())
	if !got.Pass {
		t.Fatalf("healthy run failed: %v", got.FailureReasons)
	}
	if got.StableRate != 850 {
		t.Fatalf("stable rate = %v, want 85%% of the 1000/s ceiling", got.StableRate)
	}
}

func TestMergeFailsOnGuardDefects(t *testing.T) {
	cases := map[string]func(*consumerSummary){
		"guard suppressed":     func(c *consumerSummary) { c.Guard.FalsePositives = 1 },
		"applied twice":        func(c *consumerSummary) { c.Guard.DupsMissed = 1 },
		"redelivery reapplied": func(c *consumerSummary) { c.Guard.ControlDoubleApplied = 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			run := healthyRun()
			mutate(&run.consumers[0])
			if merge(run, defaultMergeConfig()).Pass {
				t.Fatal("a guard defect must fail the verdict")
			}
		})
	}
}

func TestMergeFailsOnSoakCohortFailuresAndOnAMissingRole(t *testing.T) {
	run := healthyRun()
	run.publishers[0].Soak.Failures = 4
	if merge(run, defaultMergeConfig()).Pass {
		t.Fatal("cohort failures during the soak must fail the verdict")
	}

	got := merge(summaries{consumers: healthyRun().consumers}, defaultMergeConfig())
	if got.Pass || len(got.MissingRoleData) != 1 {
		t.Fatalf("a run with no publisher summary must fail loudly: %+v", got)
	}
}

func TestMergeSumsConsumerRepliasWithoutTreatingThemAsAPartition(t *testing.T) {
	// Every consumer pod owns its own flow consumer and receives the WHOLE lane,
	// so two pods legitimately report roughly twice what was published.
	run := healthyRun()
	run.consumers = append(run.consumers, run.consumers[0])
	got := merge(run, defaultMergeConfig())
	if got.Sequence.Delivered != 20000 {
		t.Fatalf("delivered = %d, want both pods' totals added", got.Sequence.Delivered)
	}
	if got.Consumers != 2 {
		t.Fatalf("consumers = %d, want 2", got.Consumers)
	}
}

func TestReorderingPassesButUnfilledGapsFail(t *testing.T) {
	// Overlapped cohorts commit out of order, so a gap is normally followed by
	// the regression that fills it. Equal counts must not fail the run.
	reordered := verdict{Sequence: seqCounts{Gaps: 3_000_156, Regressions: 3_000_182}}
	for _, reason := range failureReasons(reordered) {
		if strings.Contains(reason, "never delivered") {
			t.Fatalf("reordering was reported as loss: %q", reason)
		}
	}

	// Gaps with no regressions to pair with are holes nothing ever filled.
	lossy := verdict{Sequence: seqCounts{Gaps: 50_000, Regressions: 12}}
	var found bool
	for _, reason := range failureReasons(lossy) {
		if strings.Contains(reason, "never delivered") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unfilled gaps did not fail the verdict: %v", failureReasons(lossy))
	}
}
