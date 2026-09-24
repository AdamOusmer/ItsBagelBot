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

type collector struct {
	winStart unixNano
	winEnd   unixNano
	warmEnd  unixNano

	e2e      *latencySeries
	pub      *latencySeries
	del      *latencySeries
	consumed atomic.Int64

	bits     [benchPods][]uint64
	bitsOnce [benchPods]sync.Once
	maxLow   [benchPods]atomic.Uint64
	dupes    atomic.Uint64
	tracking bool
}

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
	c.record(now, sentNs, storedAt(msg))
	c.consumed.Add(1)
	c.noteSeq(seq)
	return nil
}

func (c *collector) record(now, sentNs, storedNs unixNano) {
	sec, warm := int((now-c.winStart)/1e9), now >= c.warmEnd
	c.e2e.note(sec, int64(now-sentNs), warm)
	if storedNs == 0 {
		return
	}
	c.pub.note(sec, int64(storedNs-sentNs), warm)
	c.del.note(sec, int64(now-storedNs), warm)
}

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

func storedAt(msg *bus.Message) unixNano {
	if t := msg.StoredAt(); !t.IsZero() {
		return unixNano(t.UnixNano())
	}
	return 0
}

type latencySeries struct {
	samples []int64
	idx     atomic.Int64
	secs    [][]int64
	secIdx  []atomic.Int64
}

func newLatencySeries(seconds int) *latencySeries {
	s := &latencySeries{
		samples: make([]int64, maxLatencySamples),
		secs:    make([][]int64, seconds),
		secIdx:  make([]atomic.Int64, seconds),
	}
	for i := range s.secs {
		s.secs[i] = make([]int64, secSampleCap)
	}
	return s
}

func (s *latencySeries) note(sec int, lat int64, warm bool) {
	if warm {
		if i := s.idx.Add(1); i <= int64(len(s.samples)) {
			s.samples[i-1] = lat
		}
	}
	if sec < 0 || sec >= len(s.secs) {
		return
	}
	if i := s.secIdx[sec].Add(1); i <= int64(len(s.secs[sec])) {
		s.secs[sec][i-1] = lat
	}
}

func (s *latencySeries) sorted() []int64 {
	measured := s.samples[:min(int(s.idx.Load()), len(s.samples))]
	sortAsc(measured)
	return measured
}

func (s *latencySeries) perSecond(q float64) []float64 {
	out := make([]float64, 0, len(s.secs))
	for i, sec := range s.secs {
		n := min(int(s.secIdx[i].Load()), len(sec))
		if n == 0 {
			out = append(out, 0)
			continue
		}
		sec = sec[:n]
		sortAsc(sec)
		out = append(out, float64(nearestRank(sec, q))/1e6)
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

type consumeOptions struct {
	duration    time.Duration
	startAt     unixNano
	payloadSize int
	policy      bus.ScalePolicy
	warmup      time.Duration
	feeders     int
}

func runConsume(lane benchLane, o consumeOptions) error {
	if o.startAt == 0 {
		o.startAt = unixNano(time.Now().UnixNano())
	}
	winStart, winEnd := o.startAt, o.startAt+unixNano(o.duration.Nanoseconds())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub, err := bus.NewSubscriber(lane.url, lane.group, stderrLogger())
	if err != nil {
		return err
	}

	seconds := int(o.duration.Seconds()) + 1
	c := &collector{
		winStart: winStart,
		winEnd:   winEnd,
		warmEnd:  winStart + unixNano(o.warmup.Nanoseconds()),
		e2e:      newLatencySeries(seconds),
		pub:      newLatencySeries(seconds),
		del:      newLatencySeries(seconds),
		tracking: true,
	}

	w, err := bus.ConsumeWeighted(ctx, nil, []bus.WeightedLane{{
		Sub:     sub,
		Subject: lane.subject,
		Handle:  c.handle,
	}}, o.policy, stderrLogger())
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

	emit(consumeReport{
		Consumed:     uint64(c.consumed.Load()),
		Rate:         float64(c.consumed.Load()) / o.duration.Seconds(),
		E2ENs:        summarize(c.e2e.sorted()),
		PubNs:        summarize(c.pub.sorted()),
		DelNs:        summarize(c.del.sorted()),
		PerSecP99:    c.e2e.perSecond(99),
		PerSecPubP50: c.pub.perSecond(50),
		PerSecDelP50: c.del.perSecond(50),
		Duplicates:   c.dupes.Load(),
		Missing:      c.missing(o.feeders),
		CPUUsPerMsg:  cpuMicrosPerMessage(uint64(c.consumed.Load())),
	})
	return nil
}
