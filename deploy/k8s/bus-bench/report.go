// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/codec"
	"fmt"
	"os"
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

type publishReport struct {
	Admitted    uint64       `json:"admitted"`
	Errors      uint64       `json:"errors"`
	ElapsedS    float64      `json:"elapsed_s"`
	OfferedRate float64      `json:"offered_rate"`
	CommitNs    latencyStats `json:"commit_latency_ns"`
}

type consumeReport struct {
	Consumed   uint64       `json:"consumed"`
	Rate       float64      `json:"rate"`
	E2ENs      latencyStats `json:"e2e_latency_ns"`
	Duplicates uint64       `json:"duplicates"`
}

// setupReport says what setup did: created the stream, or raised an existing
// stream's cap (recording the cap it found), or left it as it was.
type setupReport struct {
	Created          bool  `json:"created"`
	Raised           bool  `json:"raised"`
	OriginalMaxBytes int64 `json:"original_max_bytes,omitempty"`
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
