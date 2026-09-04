// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/codec"
	"fmt"
	"os"
	"syscall"
)

// Machine-readable RIG_REPORT payloads and their single emitter.

type latencyStats struct {
	Count int64   `json:"count"`
	Min   int64   `json:"min"`
	Avg   float64 `json:"avg"`
	Max   int64   `json:"max"`
	P50   int64   `json:"p50"`
	P99   int64   `json:"p99"`
}

// publishReport carries both rates on purpose: requested_rate is what -rate
// asked for (0 when unbounded) and offered_rate is what the rig actually
// admitted per second, so a step that under-delivers is visible in the report
// instead of being mistaken for a cluster ceiling. confirm_skipped counts
// samples dropped because the feeder's single confirm slot was still busy.
type publishReport struct {
	Admitted       uint64  `json:"admitted"`
	Errors         uint64  `json:"errors"`
	ElapsedS       float64 `json:"elapsed_s"`
	RequestedRate  int     `json:"requested_rate"`
	OfferedRate    float64 `json:"offered_rate"`
	ConfirmSkipped uint64  `json:"confirm_skipped"`
	// AvgCohort is messages per cohort the process's publishers sent: the
	// shape the broker committed, not the configured batch size.
	AvgCohort   float64      `json:"avg_cohort"`
	CommitNs    latencyStats `json:"commit_latency_ns"`
	CPUUsPerMsg float64      `json:"cpu_us_per_msg"`
}

type consumeReport struct {
	Consumed     uint64       `json:"consumed"`
	Rate         float64      `json:"rate"`
	E2ENs        latencyStats `json:"e2e_latency_ns"`
	PubNs        latencyStats `json:"pub_latency_ns"`
	DelNs        latencyStats `json:"deliver_latency_ns"`
	PerSecPubP50 []float64    `json:"pub_p50_ms_per_second,omitempty"`
	PerSecDelP50 []float64    `json:"deliver_p50_ms_per_second,omitempty"`
	Duplicates   uint64       `json:"duplicates"`
	PerSecP99    []float64    `json:"e2e_p99_ms_per_second,omitempty"`
	Missing      uint64       `json:"missing_sequences"`
	CPUUsPerMsg  float64      `json:"cpu_us_per_msg"`
}

// cpuMicrosPerMessage is this process's user+system CPU time divided by the
// messages it handled: a host-state-independent cost figure, where latency
// percentiles on a shared host are not.
func cpuMicrosPerMessage(messages uint64) float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil || messages == 0 {
		return 0
	}
	micros := float64(ru.Utime.Sec+ru.Stime.Sec)*1e6 + float64(ru.Utime.Usec+ru.Stime.Usec)
	return micros / float64(messages)
}

// setupReport says what setup did: created the stream, or raised an existing
// stream's cap (recording the cap it found), or left it as it was.
type setupReport struct {
	Created          bool  `json:"created"`
	Raised           bool  `json:"raised"`
	OriginalMaxBytes int64 `json:"original_max_bytes,omitempty"`
	OriginalMaxAge   int64 `json:"original_max_age,omitempty"`
}

// cleanupReport says what teardown removed and reverted.

// cleanupReport says what teardown removed and reverted.
type cleanupReport struct {
	DeletedConsumer  bool   `json:"deleted_consumer"`
	Consumer         string `json:"consumer"`
	RevertedMaxBytes bool   `json:"reverted_max_bytes"`
	MaxBytes         int64  `json:"max_bytes,omitempty"`
}

func emit(report any) {
	b, merr := codec.Marshal(report)
	if merr != nil {
		fmt.Fprintln(os.Stderr, "bus-bench: marshal report:", merr)
		os.Exit(1)
	}
	fmt.Printf("RIG_REPORT: %s\n", b)
}
