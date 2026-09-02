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
	// warmEnd bounds the cold start: samples before it feed the per-second
	// series but not the whole-run percentiles.
	warmEnd unixNano

	lat      []int64
	latIdx   atomic.Int64
	consumed atomic.Int64
	// secs is one latency slice per elapsed second of the window, so a run's
	// tail can be placed in time: a periodic spike reads differently from a
	// one-off, and neither is visible in the whole-run percentiles.
	secs   [][]int64
	secIdx []atomic.Int64

	// Per-message accounting is lock-free: a mutex here serialized every
	// handler goroutine twice per delivery inside the measurement window.
	bits     [benchPods][]uint64 // per publisher pod index (high 16 bits of seq)
	bitsOnce [benchPods]sync.Once
	maxLow   [benchPods]atomic.Uint64
	dupes    atomic.Uint64
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
	if now >= c.warmEnd {
		if i := c.latIdx.Add(1); i <= int64(len(c.lat)) {
			c.lat[i-1] = int64(now - sentNs)
		}
	}
	c.noteSecond(int((now-c.winStart)/1e9), int64(now-sentNs))
	c.consumed.Add(1)
	c.noteSeq(seq)
	return nil
}

func (c *collector) noteSecond(sec int, lat int64) {
	if sec < 0 || sec >= len(c.secs) {
		return
	}
	if i := c.secIdx[sec].Add(1); i <= int64(len(c.secs[sec])) {
		c.secs[sec][i-1] = lat
	}
}

// missing counts sequences below the highest seen that never arrived. Feeders
// stride by the feeder count, so the expected set is every low sequence up to
// maxLow that shares a feeder's residue; an approximation good to one stride.
func (c *collector) missing(feeders int) uint64 {
	var missing uint64
	for pod := range c.bits {
		maxLow := c.maxLow[pod].Load()
		if maxLow == 0 || c.bits[pod] == nil {
			continue
		}
		var seen uint64
		for w := uint64(0); w <= maxLow/64; w++ {
			seen += uint64(popcount(c.bits[pod][w]))
		}
		if expected := maxLow / uint64(max(feeders, 1)); expected > seen {
			missing += expected - seen
		}
	}
	return missing
}

func popcount(x uint64) int {
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// perSecondP99 returns each window second's p99 in milliseconds.
func (c *collector) perSecondP99() []float64 {
	out := make([]float64, 0, len(c.secs))
	for i, s := range c.secs {
		n := min(int(c.secIdx[i].Load()), len(s))
		if n == 0 {
			out = append(out, 0)
			continue
		}
		s = s[:n]
		sortAsc(s)
		out = append(out, float64(nearestRank(s, 99))/1e6)
	}
	return out
}

func (c *collector) noteSeq(seq uint64) {
	if !c.tracking {
		return
	}
	pod, low := seq>>48, seq&(1<<48-1)
	if pod >= benchPods || low >= benchSeqSpan {
		return
	}
	c.bitsOnce[pod].Do(func() { c.bits[pod] = make([]uint64, benchSeqSpan/64) })
	word, bit := low/64, uint64(1)<<(low%64)
	if atomic.OrUint64(&c.bits[pod][word], bit)&bit != 0 {
		c.dupes.Add(1)
		return
	}
	for {
		cur := c.maxLow[pod].Load()
		if low <= cur || c.maxLow[pod].CompareAndSwap(cur, low) {
			return
		}
	}
}

// stderrLogger surfaces lane warnings (fetch errors, rebuilds, floor-ack
// failures) that a no-op logger hid from every run report.
func stderrLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stderr"}
	log, err := cfg.Build()
	if err != nil {
		return zap.NewNop()
	}
	return log
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

func runConsume(lane benchLane, duration time.Duration, startAt unixNano, payloadSize int, policy bus.ScalePolicy, warmup time.Duration, feeders int) error {
	if startAt == 0 {
		startAt = unixNano(time.Now().UnixNano())
	}
	winStart, winEnd := startAt, startAt+unixNano(duration.Nanoseconds())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := bus.NewSubscriber(lane.url, lane.group, stderrLogger())
	if err != nil {
		return err
	}

	c := &collector{
		winStart: winStart,
		winEnd:   winEnd,
		warmEnd:  winStart + unixNano(warmup.Nanoseconds()),
		lat:      make([]int64, maxLatencySamples),
		secs:     make([][]int64, int(duration.Seconds())+1),
		secIdx:   make([]atomic.Int64, int(duration.Seconds())+1),
		tracking: true,
	}
	for i := range c.secs {
		c.secs[i] = make([]int64, secSampleCap)
	}

	// One consumption path only: ConsumeWeighted owns the lane binding. A
	// second raw drain here would subscribe the same durable a second time
	// (its own connection and queue membership), splitting deliveries down a
	// path whose acknowledgements race the weighted pool's.
	w, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub:     sub,
		Subject: lane.subject,
		Handle:  c.handle,
	}}, policy, stderrLogger())
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
		Duplicates: c.dupes.Load(),

		PerSecP99:   c.perSecondP99(),
		Missing:     c.missing(feeders),
		CPUUsPerMsg: cpuMicrosPerMessage(uint64(c.consumed.Load())),
	})
	return nil
}
