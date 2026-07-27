package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const rigName = "nats-stress"

// Line kinds. Both roles emit newline-delimited JSON on stdout and nothing else;
// zap logs go to stderr. Mixing them on one stream is what makes a run's output
// unparseable exactly when something interesting happened and the log got noisy.
const (
	kindTick    = "tick"
	kindStep    = "step"
	kindSummary = "summary"
)

var emitMu sync.Mutex

// emit writes one JSON line. Serialized because both roles emit from several
// goroutines and a torn line is an unrecoverable record: the reader cannot tell
// a partial object from a malformed one.
func emit(v any) {
	body, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats-stress: emit: %v\n", err)
		return
	}
	emitMu.Lock()
	defer emitMu.Unlock()
	os.Stdout.Write(append(body, '\n'))
}

// envelopeHeader is the shape every emitted line shares, so the report merge can
// route a line without knowing which role wrote it.
type envelopeHeader struct {
	Rig  string `json:"rig"`
	Role string `json:"role"`
	Kind string `json:"kind"`
}

// publisherSummary is one publisher replica's whole run. Replicas are summed by
// the merge rather than coordinating at runtime: coordination would need a
// control channel between pods, and the thing being measured is what happens
// when several independent publishers hit one stream.
type publisherSummary struct {
	envelopeHeader
	PublisherID  string       `json:"publisher_id"`
	Replicas     int          `json:"replicas"`
	Lanes        int          `json:"lanes"`
	Wire         string       `json:"publish_wire"`
	Connections  int          `json:"publish_connections"`
	PayloadBytes int          `json:"payload_bytes"`
	DupFraction  float64      `json:"dup_fraction"`
	Subject      string       `json:"subject"`
	Steps        []stepResult `json:"steps"`
	Ceiling      ceiling      `json:"ceiling"`
	StableRate   float64      `json:"stable_msgs_per_sec"`
	Soak         *stepResult  `json:"soak,omitempty"`
	SoakStable   bool         `json:"soak_latency_stable"`
	DupsDropped  int64        `json:"injected_duplicates_dropped"`
	StartedAt    time.Time    `json:"started_at"`
	EndedAt      time.Time    `json:"ended_at"`
}

// lagSample is one 1 Hz observation of how far behind the consumer is. Taken on
// a side connection, never in the handler: a StreamInfo round trip on the hot
// path would be measuring the probe.
type lagSample struct {
	AtSec         float64 `json:"at_sec"`
	StreamLastSeq uint64  `json:"stream_last_seq"`
	NumPending    uint64  `json:"num_pending"`
	AckFloorSeq   uint64  `json:"ack_floor_stream_seq"`
	Delivered     int64   `json:"delivered"`
}

// consumerSummary is one consumer replica's whole run.
type consumerSummary struct {
	envelopeHeader
	ConsumerID      string      `json:"consumer_id"`
	Stream          string      `json:"stream"`
	Subject         string      `json:"subject"`
	Group           string      `json:"group"`
	Consumer        string      `json:"consumer"`
	Routines        int         `json:"routines"`
	GuardPrefix     string      `json:"guard_prefix"`
	GuardTTL        string      `json:"guard_ttl"`
	Seq             seqCounts   `json:"sequence"`
	Guard           guardStats  `json:"guard"`
	GuardDuplicates int64       `json:"guard_duplicate_verdicts"`
	GuardFailOpen   int64       `json:"guard_fail_open"`
	GuardLatency    quantiles   `json:"guard_latency"`
	ValkeyOpsPerSec float64     `json:"valkey_claims_per_sec"`
	DecodeErrors    int64       `json:"decode_errors"`
	Lag             []lagSample `json:"lag_curve"`
	StartedAt       time.Time   `json:"started_at"`
	EndedAt         time.Time   `json:"ended_at"`
}

// verdict is what a run is read against. It is one object rather than a
// paragraph so a later run can be diffed against it mechanically.
type verdict struct {
	Rig             string       `json:"rig"`
	Kind            string       `json:"kind"`
	Publishers      int          `json:"publishers"`
	Consumers       int          `json:"consumers"`
	Ceiling         ceiling      `json:"ceiling"`
	StableRate      float64      `json:"stable_msgs_per_sec"`
	Steps           []stepResult `json:"steps"`
	Soak            *stepResult  `json:"soak,omitempty"`
	Sequence        seqCounts    `json:"sequence"`
	Guard           guardStats   `json:"guard"`
	GuardFailOpen   int64        `json:"guard_fail_open"`
	DupsDropped     int64        `json:"injected_duplicates_dropped"`
	DecodeErrors    int64        `json:"decode_errors"`
	GuardLatency    quantiles    `json:"guard_latency"`
	LagCurve        []lagSample  `json:"lag_curve"`
	LagFlat         bool         `json:"lag_flat"`
	SoakLatencyOK   bool         `json:"soak_latency_stable"`
	SoakFailures    int64        `json:"soak_cohort_failures"`
	Pass            bool         `json:"pass"`
	FailureReasons  []string     `json:"failure_reasons"`
	MissingRoleData []string     `json:"missing_role_data,omitempty"`
}

// summaries is the parsed input of a merge.
type summaries struct {
	publishers []publisherSummary
	consumers  []consumerSummary
}

// readSummaries scans newline-delimited JSON and keeps only the terminal summary
// objects. Everything else on the stream (periodic ticks, per-step lines) is
// skipped rather than rejected, so the merge can be pointed straight at a
// `kubectl logs` capture without a filtering step that could drop the one line
// that mattered.
func readSummaries(r io.Reader) (summaries, error) {
	var out summaries
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var header envelopeHeader
		if json.Unmarshal(line, &header) != nil {
			continue
		}
		if header.Rig != rigName || header.Kind != kindSummary {
			continue
		}
		if err := out.absorb(header.Role, line); err != nil {
			return out, err
		}
	}
	return out, scanner.Err()
}

func (s *summaries) absorb(role string, line []byte) error {
	switch role {
	case rolePublish:
		var summary publisherSummary
		if err := json.Unmarshal(line, &summary); err != nil {
			return fmt.Errorf("nats-stress: publisher summary: %w", err)
		}
		s.publishers = append(s.publishers, summary)
	case roleConsume:
		var summary consumerSummary
		if err := json.Unmarshal(line, &summary); err != nil {
			return fmt.Errorf("nats-stress: consumer summary: %w", err)
		}
		s.consumers = append(s.consumers, summary)
	}
	return nil
}

// mergeSteps folds the replicas' step lists into the fleet-wide one. Only step
// indices every publisher reported are kept: a replica that breached one step
// earlier than another stopped ramping there, and averaging a step some
// publishers did not run would report a fleet rate nobody offered.
func mergeSteps(publishers []publisherSummary) []stepResult {
	if len(publishers) == 0 {
		return nil
	}
	byIndex := make(map[int]*stepResult)
	seen := make(map[int]int)
	for _, p := range publishers {
		for _, step := range p.Steps {
			foldStep(byIndex, step)
			seen[step.Index]++
		}
	}
	return completeSteps(byIndex, seen, len(publishers))
}

func foldStep(byIndex map[int]*stepResult, step stepResult) {
	merged, ok := byIndex[step.Index]
	if !ok {
		clone := step
		byIndex[step.Index] = &clone
		return
	}
	merged.OfferedRate += step.OfferedRate
	merged.Offered += step.Offered
	merged.Achieved += step.Achieved
	merged.Failures += step.Failures
	merged.FlushErrors += step.FlushErrors
	merged.Truncated = merged.Truncated || step.Truncated
	// The replicas run concurrently, so the step's wall clock is the slowest of
	// them, never their sum.
	merged.ElapsedSec = max(merged.ElapsedSec, step.ElapsedSec)
	merged.Latency = worstLatency(merged.Latency, step.Latency)
}

// worstLatency keeps the replica that suffered most. Averaging quantiles across
// replicas is arithmetically meaningless and would hide the one publisher whose
// connection landed on a saturated hub member.
func worstLatency(a, b quantiles) quantiles {
	if b.P99Ms > a.P99Ms {
		return b
	}
	return a
}

func completeSteps(byIndex map[int]*stepResult, seen map[int]int, publishers int) []stepResult {
	steps := make([]stepResult, 0, len(byIndex))
	for index := 0; ; index++ {
		step, ok := byIndex[index]
		if !ok || seen[index] != publishers {
			break
		}
		steps = append(steps, *step)
	}
	return steps
}

func mergeSoak(publishers []publisherSummary) *stepResult {
	var merged *stepResult
	for _, p := range publishers {
		if p.Soak == nil {
			continue
		}
		if merged == nil {
			clone := *p.Soak
			merged = &clone
			continue
		}
		foldSoak(merged, *p.Soak)
	}
	return merged
}

func foldSoak(merged *stepResult, soak stepResult) {
	merged.OfferedRate += soak.OfferedRate
	merged.Offered += soak.Offered
	merged.Achieved += soak.Achieved
	merged.Failures += soak.Failures
	merged.FlushErrors += soak.FlushErrors
	merged.ElapsedSec = max(merged.ElapsedSec, soak.ElapsedSec)
	merged.Latency = worstLatency(merged.Latency, soak.Latency)
}

// lagFlat is the soak's core assertion: a consumer that is falling behind grows
// its pending count monotonically, so comparing the two halves of the curve
// answers "did it converge" without needing a model of the workload.
//
// The floor exists because at low pending counts the ratio is dominated by
// scheduling noise: 3 pending against 1 is a 3x growth and means nothing.
func lagFlat(curve []lagSample, tolerance float64, floor float64) bool {
	if len(curve) < 4 {
		return true // too short to say anything; do not invent a failure
	}
	if tolerance <= 0 {
		tolerance = 1.5
	}
	half := len(curve) / 2
	first := meanPending(curve[:half])
	second := meanPending(curve[half:])
	return second <= max(first, floor)*tolerance
}

func meanPending(curve []lagSample) float64 {
	if len(curve) == 0 {
		return 0
	}
	total := 0.0
	for _, sample := range curve {
		total += float64(sample.NumPending)
	}
	return total / float64(len(curve))
}

// mergeConfig carries the thresholds the verdict is judged against so the report
// subcommand can be re-run with different tolerances over a captured log instead
// of re-running the load.
type mergeConfig struct {
	Detector     ceilingDetector
	StableFrac   float64
	LagTolerance float64
	LagFloor     float64
	LatencyTol   float64
}

// merge is the whole verdict, and it is a pure function of the two roles'
// summaries: everything it asserts can therefore be re-derived from a captured
// log without the cluster.
func merge(in summaries, cfg mergeConfig) verdict {
	v := verdict{
		Rig: rigName, Kind: "verdict",
		Publishers: len(in.publishers), Consumers: len(in.consumers),
	}
	v.MissingRoleData = missingRoles(in)
	v.Steps = mergeSteps(in.publishers)
	v.Ceiling = cfg.Detector.resolve(v.Steps)
	v.StableRate = stableRate(v.Ceiling, cfg.StableFrac)
	v.Soak = mergeSoak(in.publishers)
	for _, p := range in.publishers {
		v.DupsDropped += p.DupsDropped
	}
	applyConsumerTotals(&v, in.consumers)
	v.LagFlat = lagFlat(v.LagCurve, cfg.LagTolerance, cfg.LagFloor)
	v.SoakLatencyOK = soakLatencyStable(v, cfg.LatencyTol)
	v.SoakFailures = soakFailures(v.Soak)
	v.FailureReasons = failureReasons(v)
	v.Pass = len(v.FailureReasons) == 0
	return v
}

func missingRoles(in summaries) []string {
	var missing []string
	if len(in.publishers) == 0 {
		missing = append(missing, rolePublish)
	}
	if len(in.consumers) == 0 {
		missing = append(missing, roleConsume)
	}
	return missing
}

// applyConsumerTotals sums the consumer replicas. Every pod owns its own flow
// consumer and therefore receives the WHOLE lane, so these are per-pod totals
// added together, not a partition of one lane — delivered across two consumer
// pods is expected to be roughly twice what was published.
func applyConsumerTotals(v *verdict, consumers []consumerSummary) {
	for _, c := range consumers {
		v.Sequence.Delivered += c.Seq.Delivered
		v.Sequence.Gaps += c.Seq.Gaps
		v.Sequence.Regressions += c.Seq.Regressions
		v.Sequence.Lanes = max(v.Sequence.Lanes, c.Seq.Lanes)
		v.Guard.add(c.Guard)
		v.GuardFailOpen += c.GuardFailOpen
		v.DecodeErrors += c.DecodeErrors
		v.GuardLatency = worstLatency(v.GuardLatency, c.GuardLatency)
		v.LagCurve = append(v.LagCurve, c.Lag...)
	}
}

func soakLatencyStable(v verdict, tolerance float64) bool {
	if v.Soak == nil || len(v.Steps) == 0 {
		return true
	}
	return v.Soak.Latency.stable(v.Steps[0].Latency, tolerance)
}

func soakFailures(soak *stepResult) int64 {
	if soak == nil {
		return 0
	}
	return soak.Failures + soak.FlushErrors
}

// failureReasons is the pass/fail rule, written as one list so the verdict says
// what is wrong rather than only that something is.
func failureReasons(v verdict) []string {
	var reasons []string
	reasons = appendIf(reasons, len(v.MissingRoleData) > 0,
		fmt.Sprintf("no summary from role(s) %v", v.MissingRoleData))
	reasons = appendIf(reasons, v.Guard.FalsePositives > 0,
		fmt.Sprintf("guard suppressed %d events that had never been applied", v.Guard.FalsePositives))
	reasons = appendIf(reasons, v.Guard.DupsMissed > 0,
		fmt.Sprintf("%d injected duplicates were applied twice", v.Guard.DupsMissed))
	reasons = appendIf(reasons, v.Guard.ControlDoubleApplied > 0,
		fmt.Sprintf("%d natural redeliveries were applied twice", v.Guard.ControlDoubleApplied))
	reasons = appendIf(reasons, v.SoakFailures > 0,
		fmt.Sprintf("soak reported %d cohort failures", v.SoakFailures))
	reasons = appendIf(reasons, !v.LagFlat, "consumer lag grew across the run")
	reasons = appendIf(reasons, !v.SoakLatencyOK, "soak p99 exceeded the ramp baseline")
	reasons = appendIf(reasons, v.Soak == nil, "no soak was run")
	reasons = appendIf(reasons, v.DecodeErrors > 0,
		fmt.Sprintf("%d deliveries could not be decoded", v.DecodeErrors))
	return reasons
}

func appendIf(reasons []string, condition bool, reason string) []string {
	if condition {
		return append(reasons, reason)
	}
	return reasons
}

// runReport merges the named files (or stdin when none are given) and prints the
// verdict. Files rather than a live cluster read: the run and its judgement are
// separate steps, so a captured log can be re-judged without re-running the load.
func runReport(paths []string, cfg mergeConfig) error {
	var all summaries
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	for _, path := range paths {
		parsed, err := readSummaryFile(path)
		if err != nil {
			return err
		}
		all.publishers = append(all.publishers, parsed.publishers...)
		all.consumers = append(all.consumers, parsed.consumers...)
	}
	result := merge(all, cfg)
	emit(result)
	if !result.Pass {
		return fmt.Errorf("nats-stress: verdict failed: %v", result.FailureReasons)
	}
	return nil
}

func readSummaryFile(path string) (summaries, error) {
	if path == "-" {
		return readSummaries(os.Stdin)
	}
	file, err := os.Open(path)
	if err != nil {
		return summaries{}, fmt.Errorf("nats-stress: open %q: %w", path, err)
	}
	defer file.Close()
	return readSummaries(file)
}
