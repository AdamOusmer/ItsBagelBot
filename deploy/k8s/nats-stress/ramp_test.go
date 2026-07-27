package main

import (
	"strings"
	"testing"
	"time"
)

func TestRateAtWalksTheStepsAndStops(t *testing.T) {
	plan := rampPlan{Start: 10000, Step: 5000, MaxSteps: 3}
	for index, want := range []float64{10000, 15000, 20000} {
		got, ok := plan.rateAt(index)
		if !ok || got != want {
			t.Fatalf("step %d: got (%v, %v), want (%v, true)", index, got, ok, want)
		}
	}
	if _, ok := plan.rateAt(3); ok {
		t.Fatal("step 3 must be out of range so the ramp terminates")
	}
	if _, ok := plan.rateAt(-1); ok {
		t.Fatal("negative step must not resolve")
	}
}

func TestNormalizedReplacesUnusableValues(t *testing.T) {
	plan := rampPlan{}.normalized()
	if plan.Start <= 0 || plan.Step <= 0 || plan.Hold <= 0 || plan.MaxSteps < 1 || plan.Overrun < 1 {
		t.Fatalf("zero plan did not normalize into a runnable one: %+v", plan)
	}
	kept := rampPlan{Start: 1, Step: 2, Hold: time.Second, MaxSteps: 4, Overrun: 3}
	if kept.normalized() != kept {
		t.Fatal("a fully specified plan must be left alone")
	}
}

func TestAchievedRateUsesRealElapsedNotPlannedHold(t *testing.T) {
	// The whole point of the overrun bound: a step that sent its offered count in
	// twice the wall clock achieved half the rate, not the full one.
	step := stepResult{Offered: 1000, Achieved: 1000, ElapsedSec: 2}
	if got := step.achievedRate(); got != 500 {
		t.Fatalf("achievedRate = %v, want 500", got)
	}
	if got := (stepResult{}).achievedRate(); got != 0 {
		t.Fatalf("a zero-elapsed step must report 0, got %v", got)
	}
}

func TestFailureRateIsOverAttemptsNotSuccesses(t *testing.T) {
	step := stepResult{Achieved: 90, Failures: 10}
	if got := step.failureRate(); got != 0.1 {
		t.Fatalf("failureRate = %v, want 0.1", got)
	}
	if got := (stepResult{Failures: 5}).failureRate(); got != 1 {
		t.Fatalf("an all-failure step must report 1, got %v", got)
	}
}

func TestBreachOnThroughput(t *testing.T) {
	detector := ceilingDetector{Ratio: 0.97}
	held := stepResult{OfferedRate: 1000, Achieved: 30000, ElapsedSec: 30}
	if breached, _ := detector.breach(held); breached {
		t.Fatal("a step that met its offered rate must not breach")
	}
	missed := stepResult{OfferedRate: 1000, Achieved: 25000, ElapsedSec: 30}
	breached, reason := detector.breach(missed)
	if !breached || !strings.Contains(reason, "under") {
		t.Fatalf("expected a throughput breach, got (%v, %q)", breached, reason)
	}
}

func TestBreachOnFailuresEvenAtFullRate(t *testing.T) {
	// A rate only reachable by dropping messages is not a rate.
	detector := ceilingDetector{Ratio: 0.97, MaxFailureRate: 0}
	step := stepResult{OfferedRate: 1000, Achieved: 30000, Failures: 1, ElapsedSec: 30}
	breached, reason := detector.breach(step)
	if !breached || !strings.Contains(reason, "failure rate") {
		t.Fatalf("expected a failure breach, got (%v, %q)", breached, reason)
	}
}

func TestResolveReportsBestSustainedRateIncludingTheBreachingStep(t *testing.T) {
	detector := ceilingDetector{Ratio: 0.97}
	steps := []stepResult{
		{Index: 0, OfferedRate: 1000, Achieved: 10000, ElapsedSec: 10}, // 1000/s, held
		{Index: 1, OfferedRate: 2000, Achieved: 20000, ElapsedSec: 10}, // 2000/s, held
		{Index: 2, OfferedRate: 3000, Achieved: 25000, ElapsedSec: 10}, // 2500/s, breached
	}
	got := detector.resolve(steps)
	if !got.Reached || got.StepIndex != 2 {
		t.Fatalf("expected a breach at step 2, got %+v", got)
	}
	if got.MsgPerSec != 2500 {
		t.Fatalf("ceiling = %v, want the breaching step's own 2500 (its best sustained rate)", got.MsgPerSec)
	}
}

func TestResolveWithoutBreachIsNotAReachedCeiling(t *testing.T) {
	detector := ceilingDetector{Ratio: 0.97}
	steps := []stepResult{{Index: 0, OfferedRate: 1000, Achieved: 10000, ElapsedSec: 10}}
	got := detector.resolve(steps)
	if got.Reached {
		t.Fatal("a ramp that never breached must not claim a ceiling")
	}
	if got.MsgPerSec != 1000 || !strings.Contains(got.Reason, "exhausted") {
		t.Fatalf("expected an at-least result, got %+v", got)
	}
}

func TestStableRateBacksOffTheCeiling(t *testing.T) {
	if got := stableRate(ceiling{MsgPerSec: 100000}, 0.85); got != 85000 {
		t.Fatalf("stableRate = %v, want 85000", got)
	}
	if got := stableRate(ceiling{MsgPerSec: 100000}, 0); got != 85000 {
		t.Fatalf("an unusable fraction must fall back to 0.85, got %v", got)
	}
}
