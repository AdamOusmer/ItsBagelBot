package main

import (
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// quantiles is a latency distribution reported in milliseconds. Milliseconds
// because every operating number in this system's runbooks is in milliseconds,
// and a report that needs unit conversion before it can be compared is a report
// nobody compares.
type quantiles struct {
	Count   int64   `json:"count"`
	P50Ms   float64 `json:"p50_ms"`
	P95Ms   float64 `json:"p95_ms"`
	P99Ms   float64 `json:"p99_ms"`
	MaxMs   float64 `json:"max_ms"`
	Sampled int64   `json:"sampled"`
}

// sampler collects publish-to-PubAck durations from every lane.
//
// It samples rather than timing every message for two reasons that both change
// the answer: a confirmed publish blocks its caller until the cohort commits, so
// timing all of them would convert the open-loop publisher into a closed-loop one
// and cap the measured rate at one cohort round-trip per lane; and holding a
// timestamp per message at six figures a second is allocation pressure the
// measurement would then be reporting on itself.
//
// Above Capacity it switches to reservoir sampling, so a long soak keeps a
// uniform sample of its whole window instead of the first Capacity events —
// otherwise the p99 of a five-minute soak would be the p99 of its first second.
type sampler struct {
	mu       sync.Mutex
	values   []time.Duration
	capacity int
	seen     int64
	rng      *rand.Rand
}

func newSampler(capacity int) *sampler {
	if capacity < 1 {
		capacity = 1
	}
	return &sampler{
		values:   make([]time.Duration, 0, capacity),
		capacity: capacity,
		// Seeded independently of the payload generator: a shared source would
		// correlate which events are sampled with which are duplicated, and the
		// duplicate path is measurably slower.
		rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
}

func (s *sampler) record(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen++
	if len(s.values) < s.capacity {
		s.values = append(s.values, d)
		return
	}
	if slot := s.rng.Int64N(s.seen); slot < int64(s.capacity) {
		s.values[slot] = d
	}
}

// drain returns the distribution and empties the reservoir, so consecutive steps
// report their own latency rather than a running average over the whole ramp.
func (s *sampler) drain() quantiles {
	s.mu.Lock()
	values := s.values
	seen := s.seen
	s.values = make([]time.Duration, 0, s.capacity)
	s.seen = 0
	s.mu.Unlock()
	return summarize(values, seen)
}

func summarize(values []time.Duration, seen int64) quantiles {
	if len(values) == 0 {
		return quantiles{Sampled: seen}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return quantiles{
		Count:   int64(len(sorted)),
		P50Ms:   millisAt(sorted, 0.50),
		P95Ms:   millisAt(sorted, 0.95),
		P99Ms:   millisAt(sorted, 0.99),
		MaxMs:   float64(sorted[len(sorted)-1].Microseconds()) / 1000,
		Sampled: seen,
	}
}

// millisAt is the nearest-rank quantile: no interpolation, so a reported p99 is
// always a duration something actually took.
func millisAt(sorted []time.Duration, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(q * float64(len(sorted)))
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return float64(sorted[rank].Microseconds()) / 1000
}

// stable reports whether a soak's distribution held against the ramp's. It
// compares p99 only: p50 barely moves under queueing while p99 is where a
// system that is quietly failing to keep up shows it first.
func (q quantiles) stable(baseline quantiles, tolerance float64) bool {
	if baseline.P99Ms <= 0 || q.Count == 0 {
		return true // nothing to compare against; do not invent a failure
	}
	if tolerance <= 0 {
		tolerance = 1.5
	}
	return q.P99Ms <= baseline.P99Ms*tolerance
}
