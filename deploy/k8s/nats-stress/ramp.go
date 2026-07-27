package main

import (
	"fmt"
	"time"
)

// rampPlan is the deterministic self-ramp: hold each rate for Hold, then add
// Step, up to MaxSteps. Deterministic on purpose — a run that adapts its own
// schedule to what it measured cannot be compared against the previous run, and
// comparing runs is the only way to tell a regression from a noisy afternoon.
type rampPlan struct {
	Start    float64       // offered msg/s at step 0, fleet-wide
	Step     float64       // msg/s added per step
	Hold     time.Duration // how long each step is offered
	MaxSteps int
	// Overrun bounds how far past Hold a step may run while it drains its
	// offered count. Past this the step is cut short and reported truncated: a
	// broker at half the offered rate would otherwise stretch one step to twice
	// its wall clock and hide the collapse inside a single long sample.
	Overrun float64
}

// rateAt is the offered rate for one step index. Out-of-range indices report
// ok=false rather than clamping, so a caller that forgets the bound ramps
// forever instead of hammering the last rate.
func (p rampPlan) rateAt(index int) (float64, bool) {
	if index < 0 || index >= p.MaxSteps {
		return 0, false
	}
	return p.Start + float64(index)*p.Step, true
}

// normalized replaces the values a flag typo can produce with the smallest
// values that still describe a ramp, so a bad flag degrades to a short run
// instead of a division by zero mid-measurement.
func (p rampPlan) normalized() rampPlan {
	if p.Start <= 0 {
		p.Start = 1000
	}
	if p.Step <= 0 {
		p.Step = p.Start
	}
	if p.Hold <= 0 {
		p.Hold = 30 * time.Second
	}
	if p.MaxSteps < 1 {
		p.MaxSteps = 1
	}
	if p.Overrun < 1 {
		p.Overrun = 2
	}
	return p
}

// stepResult is one measured step. Offered is what the plan demanded, Achieved
// what the publisher actually admitted, and ElapsedSec the wall clock it took —
// all three are needed because a step that keeps up and a step that takes twice
// as long to send the same count are the same two counters and different
// answers.
type stepResult struct {
	Index       int       `json:"index"`
	OfferedRate float64   `json:"offered_msgs_per_sec"`
	Offered     int64     `json:"offered"`
	Achieved    int64     `json:"achieved"`
	Failures    int64     `json:"failures"`
	FlushErrors int64     `json:"flush_errors"`
	ElapsedSec  float64   `json:"elapsed_sec"`
	Truncated   bool      `json:"truncated"`
	Latency     quantiles `json:"publish_ack_latency"`
	Soak        bool      `json:"soak"`
}

// achievedRate is throughput against the wall clock the step really used, not
// against the wall clock it was planned for. Dividing by the planned Hold would
// report a step that overran as if it had kept up.
func (r stepResult) achievedRate() float64 {
	if r.ElapsedSec <= 0 {
		return 0
	}
	return float64(r.Achieved) / r.ElapsedSec
}

// failureRate is failures per attempted publish, which is the quantity the
// threshold is expressed in. Attempts, not achievements: dividing by successes
// would send the ratio to infinity exactly when it matters.
func (r stepResult) failureRate() float64 {
	attempts := r.Achieved + r.Failures
	if attempts <= 0 {
		return 0
	}
	return float64(r.Failures) / float64(attempts)
}

// ceilingDetector decides when the offered rate has stopped being achievable.
type ceilingDetector struct {
	// Ratio is the fraction of the offered rate a step must sustain. Below it the
	// broker is absorbing less than it is being handed and the queue in front of
	// it is doing the pacing, which is the definition of the ceiling.
	Ratio float64
	// MaxFailureRate breaches independently of throughput: a step can hold its
	// rate while failing cohorts, and a rate only reachable by dropping messages
	// is not a rate.
	MaxFailureRate float64
}

func (d ceilingDetector) normalized() ceilingDetector {
	if d.Ratio <= 0 || d.Ratio > 1 {
		d.Ratio = 0.97
	}
	if d.MaxFailureRate < 0 {
		d.MaxFailureRate = 0
	}
	return d
}

// breach reports whether one step failed to hold its offered rate, and why. The
// reason is carried out rather than logged here so the summary can state which
// of the two conditions ended the ramp — they mean different things to whoever
// reads it.
func (d ceilingDetector) breach(r stepResult) (bool, string) {
	d = d.normalized()
	if r.failureRate() > d.MaxFailureRate {
		return true, fmt.Sprintf("failure rate %.4f exceeded %.4f", r.failureRate(), d.MaxFailureRate)
	}
	if r.achievedRate() < r.OfferedRate*d.Ratio {
		return true, fmt.Sprintf("achieved %.0f msg/s under %.0f%% of offered %.0f msg/s",
			r.achievedRate(), d.Ratio*100, r.OfferedRate)
	}
	return false, ""
}

// ceiling is the verdict of a completed ramp.
type ceiling struct {
	Reached   bool    `json:"reached"`
	StepIndex int     `json:"step_index"`
	Reason    string  `json:"reason"`
	MsgPerSec float64 `json:"msgs_per_sec"`
}

// resolve turns a finished ramp into a ceiling. The ceiling is the best rate any
// step actually sustained, including the step that breached: a step can breach
// on failures while still having pushed more than every step before it, and
// reporting the previous step's rate would understate the broker.
//
// A ramp that never breached reports Reached=false with the best rate seen, so
// the caller can say "at least this" instead of inventing a limit the run never
// found.
func (d ceilingDetector) resolve(results []stepResult) ceiling {
	best := 0.0
	for _, r := range results {
		best = max(best, r.achievedRate())
		breached, reason := d.breach(r)
		if breached {
			return ceiling{Reached: true, StepIndex: r.Index, Reason: reason, MsgPerSec: best}
		}
	}
	return ceiling{MsgPerSec: best, Reason: "ramp exhausted without a breach"}
}

// stableRate is the rate the soak runs at: a fixed fraction below the ceiling,
// because the ceiling is by construction the point where the system stopped
// keeping up and soaking at it measures the queue, not the system.
func stableRate(c ceiling, fraction float64) float64 {
	if fraction <= 0 || fraction > 1 {
		fraction = 0.85
	}
	return c.MsgPerSec * fraction
}
