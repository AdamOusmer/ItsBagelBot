// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"testing"
	"time"
)

const testPod = 2

func openCollector() *collector {
	now := unixNano(time.Now().UnixNano())
	return &collector{
		winStart: now - unixNano(time.Minute), winEnd: now + unixNano(time.Minute),
		e2e: newLatencySeries(1), proposed: newLatencySeries(1), received: newLatencySeries(1),
	}
}

func globalSeqs(n int) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = uint64(testPod)<<48 | uint64(i+1)
	}
	return out
}

func published(seqs []uint64) podChecksum {
	run := &sampleRun{}
	for _, s := range seqs {
		run.count(nil, s)
	}
	return podChecksum{Pod: testPod, Count: run.tally.admitted, SeqSum: run.sums.sum.Load(), SeqXor: run.sums.xor.Load()}
}

func received(t *testing.T, parts ...[]uint64) podChecksum {
	t.Helper()
	total := podChecksum{Pod: testPod}
	for _, part := range parts {
		c := openCollector()
		for _, s := range part {
			_ = c.handle(bus.NewMessage("", buildPayload(s, unixNano(time.Now().UnixNano()), 64)))
		}
		for _, p := range c.podChecksums() {
			total.Count += p.Count
			total.SeqSum += p.SeqSum
			total.SeqXor ^= p.SeqXor
		}
	}
	return total
}

func TestReconcileAcrossConsumerPods(t *testing.T) {
	seqs := globalSeqs(1000)
	cases := []struct {
		name  string
		parts [][]uint64
		match bool
	}{
		{"split across pods", [][]uint64{seqs[:400], seqs[400:]}, true},
		{"cross-pod duplicate", [][]uint64{seqs[:300], seqs[250:]}, false},
		{"lost tail", [][]uint64{seqs[:400], seqs[400:999]}, false},
	}
	want := published(seqs)
	for _, tc := range cases {
		if got := received(t, tc.parts...); (got == want) != tc.match {
			t.Errorf("%s: received %+v, published %+v, want match=%v", tc.name, got, want, tc.match)
		}
	}
}
