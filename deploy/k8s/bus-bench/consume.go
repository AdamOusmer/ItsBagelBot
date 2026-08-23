// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"context"
	"encoding/binary"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Consume mode: one weighted lane drains the subject while a collector
// measures end-to-end latency and duplicate deliveries inside its window.

type collector struct {
	winStart unixNano
	winEnd   unixNano

	lat      []int64
	latIdx   atomic.Int64
	consumed atomic.Int64

	mu       sync.Mutex
	seen     map[uint64]struct{}
	dupes    uint64
	tracking bool
}

// measuring reports whether deliveries arriving at now count toward this
// run's measurement window.

// measuring reports whether deliveries arriving at now count toward this
// run's measurement window.
func (c *collector) measuring(now unixNano) bool {
	return now >= c.winStart && now < c.winEnd
}

func (c *collector) handle(msg *bus.Message) error {
	now := unixNano(time.Now().UnixNano())
	defer msg.Ack()

	p := msg.Payload
	if len(p) < 16 || !c.measuring(now) {
		return nil
	}
	seq := binary.BigEndian.Uint64(p[0:8])
	sentNs := unixNano(binary.BigEndian.Uint64(p[8:16]))
	if i := c.latIdx.Add(1); i <= int64(len(c.lat)) {
		c.lat[i-1] = int64(now - sentNs)
	}
	c.consumed.Add(1)
	c.noteSeq(seq)
	return nil
}

func (c *collector) noteSeq(seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.tracking || len(c.seen) >= dupMapLimit {
		return
	}
	if _, dup := c.seen[seq]; dup {
		c.dupes++
		return
	}
	c.seen[seq] = struct{}{}
}

func summarize(sorted []int64) latencyStats {
	if len(sorted) == 0 {
		return latencyStats{}
	}
	var sum int64
	for _, v := range sorted {
		sum += v
	}
	n := int64(len(sorted))
	return latencyStats{
		Count: n,
		Min:   sorted[0],
		Avg:   float64(sum) / float64(n),
		Max:   sorted[n-1],
		P50:   nearestRank(sorted, 50),
		P99:   nearestRank(sorted, 99),
	}
}

func nearestRank(sorted []int64, p float64) int64 {
	n := len(sorted)
	rank := int(math.Ceil(p / 100 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func sortAsc(v []int64) {
	slices.Sort(v)
}

// publishOpts bundles one publish-mode invocation. Every flag feeds the same
// run, so they travel as a struct rather than as a nine-argument signature.

func runConsume(lane benchLane, duration time.Duration, startAt unixNano, payloadSize int) error {
	if startAt == 0 {
		startAt = unixNano(time.Now().UnixNano())
	}
	winStart, winEnd := startAt, startAt+unixNano(duration.Nanoseconds())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := bus.NewSubscriber(lane.url, lane.group, zap.NewNop())
	if err != nil {
		return err
	}

	c := &collector{
		winStart: winStart,
		winEnd:   winEnd,
		lat:      make([]int64, maxLatencySamples),
		seen:     make(map[uint64]struct{}, 1024),
		tracking: true,
	}
	// One consumption path only: ConsumeWeighted owns the lane binding. A
	// second raw drain here would subscribe the same durable a second time
	// (its own connection and queue membership), splitting deliveries down a
	// path whose acknowledgements race the weighted pool's.
	w, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub:     sub,
		Subject: lane.subject,
		Handle:  c.handle,
	}}, bus.ScalePolicy{MinRoutines: 256, MaxRoutines: 512}, zap.NewNop())
	if err != nil {
		return err
	}

	winStart.wait()
	winEnd.wait()

	cancel()
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = w.Drain(drainCtx)
	drainCancel()
	_ = sub.Close()

	measured := c.lat[:c.latIdx.Load()]
	sortAsc(measured)
	emit(consumeReport{
		Consumed:   uint64(c.consumed.Load()),
		Rate:       float64(c.consumed.Load()) / duration.Seconds(),
		E2ENs:      summarize(measured),
		Duplicates: c.dupes,
	})
	return nil
}
