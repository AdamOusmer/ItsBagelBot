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
	// Skipped is the samples that were due and never taken, because the bounded
	// pool that times them was full. It is reported next to the distribution
	// because it is the distribution's own bias: a step that skipped most of its
	// samples measured whichever publishes happened to find a free slot. The
	// consumer's guard sampler never skips, so it always reports zero.
	Skipped int64 `json:"skipped"`
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
// Sampling alone was not enough. Even one in 512, taken on the lane goroutine,
// became the run's binding constraint once the round trip grew: at 152 ms it held
// a fleet-wide run to 54k msg/s, which is the sampler's number and not the
// broker's. The confirmed publish now runs off the lane in a bounded pool, and
// what the pool refuses is counted here.
//
// Above Capacity it switches to reservoir sampling, so a long soak keeps a
// uniform sample of its whole window instead of the first Capacity events —
// otherwise the p99 of a five-minute soak would be the p99 of its first second.
type sampler struct {
	mu       sync.Mutex
	values   []time.Duration
	capacity int
	seen     int64
	skipped  int64
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

// skip records a sample that was due and never taken. Counted rather than
// ignored: the alternative to skipping is blocking the lane that owed the
// sample, and a reader has to be able to tell a distribution built from most of
// its samples from one built from a handful.
func (s *sampler) skip() {
	s.mu.Lock()
	s.skipped++
	s.mu.Unlock()
}

// drain returns the distribution and empties the reservoir, so consecutive steps
// report their own latency rather than a running average over the whole ramp.
func (s *sampler) drain() quantiles {
	s.mu.Lock()
	values := s.values
	seen, skipped := s.seen, s.skipped
	s.values = make([]time.Duration, 0, s.capacity)
	s.seen, s.skipped = 0, 0
	s.mu.Unlock()
	return summarize(values, seen, skipped)
}

func summarize(values []time.Duration, seen, skipped int64) quantiles {
	if len(values) == 0 {
		return quantiles{Sampled: seen, Skipped: skipped}
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
		Skipped: skipped,
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
