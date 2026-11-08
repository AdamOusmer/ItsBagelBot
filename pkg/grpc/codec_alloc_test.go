//go:build !race

// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import "testing"

// Race instrumentation intentionally drops sync.Pool entries. Measure the
// steady-state allocation budget without that instrumentation; lifecycle and
// ownership tests still run with -race.
func TestSmallMarshalAllocationBudget(t *testing.T) {
	message := samplePacket()
	allocations := testing.AllocsPerRun(1000, func() {
		data, err := (vtCodec{}).Marshal(message)
		if err != nil {
			panic(err)
		}
		data.Free()
	})
	if allocations != 1 {
		t.Fatalf("warmed small marshal allocated %g times, want one BufferSlice", allocations)
	}
}
