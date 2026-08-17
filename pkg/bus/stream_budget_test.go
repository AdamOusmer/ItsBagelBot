// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestMemoryStreamsFitTheHubMemoryBudget(t *testing.T) {
	// Under R3 every hub peer holds a full replica, so the per-node floor is the
	// sum of every memory-backed stream's MaxBytes — not a single node's share.
	//
	// max_mem accounts for that sum and nothing else, which is why this test
	// models the terms the broker leaves out. On a memory stream each peer also
	// carries a memStore RAFT WAL (createRaftGroup's else-branch), which between
	// snapshots approaches a second copy of the stream; staged atomic batches for
	// R>1 are a newMemStore that RegisterStorageUpdates never sees; and every
	// stream has its own ingest queue bounded by max_buffered_size. None of that
	// is registered with account storage, so max_mem cannot back-pressure it —
	// only the pod's memory limit can, by OOM-killing the member.
	//
	// Keep these three constants in step with deploy/messaging/nats-server.conf and the
	// nats StatefulSet's limits.
	//
	// Note the per-STREAM term: partitioning a lane onto a second stream is not
	// free here even when the byte budget is split rather than raised, because
	// each stream carries its own ingest queue. That is the cost the ingress
	// partition pays, and this test is where it has to stay affordable.
	const (
		maxMem          = 4 << 30   // jetstream.max_mem
		podMemoryLimit  = 5 << 30   // nats container limits.memory
		bufferedPerNode = 128 << 20 // jetstream.max_buffered_size, per stream
		runtimeReserve  = 1 << 30   // Go heap, connection buffers, dedup ids
	)

	streamBytes, streams := memoryReservation(t)

	// What max_mem sees.
	if streamBytes >= maxMem/2 {
		t.Fatalf("memory-backed streams reserve %d bytes per node, leaving too little of the %d max_mem for broker state",
			streamBytes, int64(maxMem))
	}

	// What the kernel sees: stream bytes + an equal-sized RAFT WAL ceiling +
	// one ingest queue per stream + room for the process itself.
	worstCase := 2*streamBytes + streams*bufferedPerNode + runtimeReserve
	if worstCase >= podMemoryLimit {
		t.Fatalf("worst-case per-member memory %d (streams %d + WAL + %d ingest queues) exceeds the %d pod limit",
			worstCase, streamBytes, streams, int64(podMemoryLimit))
	}
}

// memoryReservation returns the bytes every hub peer reserves for memory-backed
// streams, and the catalog stream count each of which carries its own ingest
// queue.
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

// TestIngressPartitionSplitsOneGigabyteRatherThanAddingOne is the memory
// contract of the partition, stated as the arithmetic an operator would
// otherwise have to redo: the two lane streams together reserve exactly what the
// single stream reserved before the split. Under R3 that reservation is per hub
// peer, so a partition that quietly "rounded up" each half to a comfortable
// number would raise the per-node floor by that rounding on all three members at
// once, against a max_mem nobody edited.
func TestIngressPartitionSplitsOneGigabyteRatherThanAddingOne(t *testing.T) {
	const unpartitioned = 1 << 30 // what TWITCH_INGRESS alone held before the split

	premium := streamConfig(TwitchIngressStream).MaxBytes
	standard := streamConfig(TwitchIngressStandardStream).MaxBytes
	if got := premium + standard; got != unpartitioned {
		t.Fatalf("ingress lane streams reserve %d bytes per node, want the unpartitioned %d",
			got, int64(unpartitioned))
	}
	// The bulk lane gets the bulk of the budget: standard carries every
	// non-premium broadcaster plus every event with no extractable broadcaster,
	// and the byte cap is the consumer lag budget.
	if standard <= premium {
		t.Fatalf("standard lane budget %d does not exceed the premium/stream/status budget %d",
			standard, premium)
	}
	// The per-subject cap on the shared stream must stay under its byte cap, or
	// the isolation it provides is theoretical: one lane would have to evict its
	// neighbours before it could wrap itself.
	const wireBytesPerEvent = 865 // live-measured on the R3 lane
	if capBytes := streamConfig(TwitchIngressStream).MaxMsgsPerSubject * wireBytesPerEvent; capBytes >= premium {
		t.Fatalf("per-subject cap is %d bytes against a %d stream cap; a flooded lane evicts its neighbours first",
			capBytes, premium)
	}
	// And the partition's own stream must NOT carry that cap: with one subject it
	// would only be a second, tighter ceiling on the same bytes.
	if got := streamConfig(TwitchIngressStandardStream).MaxMsgsPerSubject; got != -1 {
		t.Fatalf("standard partition per-subject cap = %d, want the unlimited sentinel on a single-subject stream", got)
	}
}
