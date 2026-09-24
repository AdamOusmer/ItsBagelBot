// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestMemoryStreamsFitTheHubMemoryBudget(t *testing.T) {
	const (
		maxMem          = 4 << 30
		podMemoryLimit  = 8 << 30
		bufferedPerNode = 128 << 20
		runtimeReserve  = 1 << 30
	)

	streamBytes, streams := memoryReservation(t)

	if streamBytes >= maxMem/2 {
		t.Fatalf("memory-backed streams reserve %d bytes per node, leaving too little of the %d max_mem for broker state",
			streamBytes, int64(maxMem))
	}

	worstCase := 2*streamBytes + streams*bufferedPerNode + runtimeReserve
	if worstCase >= podMemoryLimit {
		t.Fatalf("worst-case per-member memory %d (streams %d + WAL + %d ingest queues) exceeds the %d pod limit",
			worstCase, streamBytes, streams, int64(podMemoryLimit))
	}
}

func memoryReservation(t *testing.T) (streamBytes, streams int64) {
	t.Helper()
	for _, spec := range fleetStreamSpecs() {
		cfg := streamConfig(spec)
		streams++
		if cfg.Storage != jsapi.MemoryStorage {
			continue
		}
		if cfg.MaxBytes <= 0 {
			t.Fatalf("memory stream %s has no byte cap; it can exhaust max_mem", spec.Name)
		}
		streamBytes += cfg.MaxBytes
	}
	return streamBytes, streams
}

func TestIngressPartitionSplitsOneGigabyteRatherThanAddingOne(t *testing.T) {
	const unpartitioned = 1 << 30

	premium := streamConfig(TwitchIngressStream).MaxBytes
	standard := streamConfig(TwitchIngressStandardStream).MaxBytes
	if got := premium + standard; got != unpartitioned {
		t.Fatalf("ingress lane streams reserve %d bytes per node, want the unpartitioned %d",
			got, int64(unpartitioned))
	}
	if standard <= premium {
		t.Fatalf("standard lane budget %d does not exceed the premium/stream/status budget %d",
			standard, premium)
	}
	const wireBytesPerEvent = 865
	if capBytes := streamConfig(TwitchIngressStream).MaxMsgsPerSubject * wireBytesPerEvent; capBytes >= premium {
		t.Fatalf("per-subject cap is %d bytes against a %d stream cap; a flooded lane evicts its neighbours first",
			capBytes, premium)
	}
	if got := streamConfig(TwitchIngressStandardStream).MaxMsgsPerSubject; got != -1 {
		t.Fatalf("standard partition per-subject cap = %d, want the unlimited sentinel on a single-subject stream", got)
	}
}
