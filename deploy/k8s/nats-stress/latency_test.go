package main

import (
	"testing"
	"time"
)

func TestSummarizeUsesNearestRank(t *testing.T) {
	values := make([]time.Duration, 0, 100)
	for i := 1; i <= 100; i++ {
		values = append(values, time.Duration(i)*time.Millisecond)
	}
	got := summarize(values, 100)
	// Nearest rank, no interpolation: a reported quantile is always a duration
	// something actually took.
	if got.P50Ms != 51 || got.P95Ms != 96 || got.P99Ms != 100 || got.MaxMs != 100 {
		t.Fatalf("quantiles = %+v", got)
	}
	if got.Count != 100 || got.Sampled != 100 {
		t.Fatalf("counts = %+v", got)
	}
}

func TestSummarizeOfNothingReportsTheAttempt(t *testing.T) {
	got := summarize(nil, 7)
	if got.Count != 0 || got.Sampled != 7 {
		t.Fatalf("an empty reservoir must still report what it saw: %+v", got)
	}
}

func TestSamplerDrainResetsBetweenSteps(t *testing.T) {
	// Consecutive steps must report their own latency, not a running average
	// over the whole ramp.
	s := newSampler(16)
	s.record(10 * time.Millisecond)
	if first := s.drain(); first.Count != 1 {
		t.Fatalf("first drain = %+v", first)
	}
	if second := s.drain(); second.Count != 0 {
		t.Fatalf("second drain must be empty, got %+v", second)
	}
}

func TestSamplerBoundsItsReservoir(t *testing.T) {
	s := newSampler(64)
	for i := 0; i < 10000; i++ {
		s.record(time.Duration(i) * time.Microsecond)
	}
	got := s.drain()
	if got.Count != 64 {
		t.Fatalf("reservoir grew past its capacity: %d", got.Count)
	}
	if got.Sampled != 10000 {
		t.Fatalf("the population size must survive sampling: %d", got.Sampled)
	}
}

func TestStableComparesP99AgainstTheBaseline(t *testing.T) {
	baseline := quantiles{Count: 10, P99Ms: 10}
	if !(quantiles{Count: 10, P99Ms: 14}).stable(baseline, 1.5) {
		t.Fatal("p99 inside the tolerance must be stable")
	}
	if (quantiles{Count: 10, P99Ms: 16}).stable(baseline, 1.5) {
		t.Fatal("p99 past the tolerance must not be stable")
	}
	if !(quantiles{Count: 10, P99Ms: 999}).stable(quantiles{}, 1.5) {
		t.Fatal("with no baseline there is nothing to compare; do not invent a failure")
	}
	if !(quantiles{}).stable(baseline, 1.5) {
		t.Fatal("with no samples there is nothing to compare")
	}
}
