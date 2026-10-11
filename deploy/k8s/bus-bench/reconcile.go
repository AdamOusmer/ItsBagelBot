// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import "sync/atomic"

type seqAccumulator struct {
	count atomic.Uint64
	sum   atomic.Uint64
	xor   atomic.Uint64
}

type podChecksum struct {
	Pod    int    `json:"pod"`
	Count  uint64 `json:"count"`
	SeqSum uint64 `json:"seq_sum"`
	SeqXor uint64 `json:"seq_xor"`
}

func mixSeq(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func (a *seqAccumulator) add(seq uint64) {
	a.count.Add(1)
	a.sum.Add(seq)
	mixed := mixSeq(seq)
	for {
		cur := a.xor.Load()
		if a.xor.CompareAndSwap(cur, cur^mixed) {
			return
		}
	}
}

func (c *collector) noteChecksum(seq uint64) {
	if pod := seq >> 48; pod < benchPods {
		c.perPod[pod].add(seq)
	}
}

func (c *collector) podChecksums() []podChecksum {
	out := []podChecksum{}
	for pod := range c.perPod {
		acc := &c.perPod[pod]
		if n := acc.count.Load(); n > 0 {
			out = append(out, podChecksum{Pod: pod, Count: n, SeqSum: acc.sum.Load(), SeqXor: acc.xor.Load()})
		}
	}
	return out
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
