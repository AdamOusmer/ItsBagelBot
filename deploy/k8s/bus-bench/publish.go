// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Publish mode: feeders drive every pooled publisher connection under their
// own partition to the deadline, sampling confirmed-publish commit latency.

// publishOpts bundles one publish-mode invocation. Every flag feeds the same
// run, so they travel as a struct rather than as a nine-argument signature.
type publishOpts struct {
	lane         benchLane
	duration     time.Duration
	startAt      unixNano
	payloadSize  int
	confirmEvery int
	rate         int
	// idPad lengthens the message id header by that many bytes. The broker
	// scans the whole header block once per key it checks (~30 bytes.Index
	// calls per message on the stream leader), so header length is a direct
	// cost on the serialized ingest path: measured 2026-09-03, +300 bytes took
	// one R3 stream from 127k to 117k msg/s with nothing else changed.
	idPad     int
	paceEvery int
	podIndex  int
	feeders   int
}

func buildPayload(seq uint64, sent unixNano, size int) []byte {
	if size < 16 {
		size = 16
	}
	buf := make([]byte, size)
	binary.BigEndian.PutUint64(buf[0:8], seq)
	binary.BigEndian.PutUint64(buf[8:16], uint64(sent))
	for i := 16; i < size; i++ {
		buf[i] = byte(seq>>uint(i%7)) ^ byte(i)
	}
	return buf
}

func runPublish(o publishOpts) error {
	o.startAt.wait()
	windowStart := time.Now()
	deadline := windowStart.Add(o.duration)

	pub, err := bus.NewPublisher(o.lane.url, stderrLogger())
	if err != nil {
		return err
	}
	defer pub.Close()

	ctx := context.Background()
	run := &sampleRun{pub: pub, opts: o, deadline: deadline, pad: strings.Repeat("x", o.idPad)}
	samples := run.drive(ctx)

	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = pub.Flush(flushCtx)
	cancel()

	elapsed := time.Since(windowStart)
	sortAsc(samples)
	emit(publishReport{
		Admitted:       run.tally.admitted,
		Errors:         run.tally.errors,
		ElapsedS:       elapsed.Seconds(),
		RequestedRate:  o.rate,
		OfferedRate:    float64(run.tally.admitted) / elapsed.Seconds(),
		ConfirmSkipped: run.tally.confirmSkipped,
		AvgCohort:      avgCohort(),
		CommitNs:       summarize(samples),
		CPUUsPerMsg:    cpuMicrosPerMessage(run.tally.admitted),
	})
	return nil
}

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// their merged commit-latency samples.

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// in aggregate; disabled when no rate was requested.

// feedPacer spaces a feeder's publishes so the pool offers rate/feeders msg/s
// in aggregate; disabled when no rate was requested.
//
// Paced per GROUP of every messages, not per message. At -rate 150000 -feeders 8
// the per-message stride is 53µs, far under this host's time.Sleep granularity
// (~1ms), so a sleep per message overshot every slot and the rig admitted only
// 122-137k/s of a requested 150k. Sleeping once per group amortizes that
// granularity over `every` messages. The slot accumulates instead of resetting
// to now, so time lost to a slow publish is caught up by the following groups
// rather than dropped — that part is load-bearing, do not "simplify" it to a
// sleep of one stride.
type feedPacer struct {
	on     bool
	slot   time.Time
	stride time.Duration
	every  int
	n      int
}

func newFeedPacer(rate, feeders, every int) feedPacer {
	if rate <= 0 {
		return feedPacer{}
	}
	if every < 1 {
		every = 1
	}
	return feedPacer{
		on:     true,
		slot:   time.Now(),
		stride: time.Second * time.Duration(feeders) / time.Duration(rate),
		every:  every,
	}
}

// benchMessage is one message the rig sends: where, with what identity and
// payload, and whether its commit latency is sampled.
type benchMessage struct {
	subject   string
	id        string
	body      []byte
	confirmed bool
}

// sendOne puts one message on the wire and reports how long the call took.
func (r *sampleRun) sendOne(ctx context.Context, m benchMessage) (time.Duration, error) {
	t0 := time.Now()
	var err error
	if m.confirmed {
		err = bus.PublishConfirmed(ctx, r.pub, bus.Publication{Subject: m.subject, ID: m.id, Payload: m.body})
	} else {
		err = bus.PublishRaw(ctx, r.pub, m.subject, m.body)
	}
	return time.Since(t0), err
}

// confirmLane carries one feeder's confirmed publishes off that feeder's own
// goroutine. bus.PublishConfirmed blocks for the cohort's whole commit round
// trip (p99 46-160ms measured), so confirming inline stalled the feeder for
// tens of milliseconds every confirmEvery messages and starved the pacer. The
// bound is one outstanding confirm per feeder: when the slot is still held the
// message is published raw and counted as confirm_skipped, so a slow commit
// costs a sample and never an unbounded goroutine pile-up.
type confirmLane struct {
	slot    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	samples []int64
}

func newConfirmLane() *confirmLane {
	l := &confirmLane{slot: make(chan struct{}, 1)}
	l.slot <- struct{}{}
	return l
}

// take claims the lane's single confirm slot, reporting whether it was free.
func (l *confirmLane) take() bool {
	select {
	case <-l.slot:
		return true
	default:
		return false
	}
}

func (l *confirmLane) release() { l.slot <- struct{}{} }

func (l *confirmLane) record(ns int64) {
	l.mu.Lock()
	l.samples = append(l.samples, ns)
	l.mu.Unlock()
}

// drain waits for the outstanding confirm, if any, then returns the samples.
func (l *confirmLane) drain() []int64 {
	l.wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.samples
}

// sampleRun is one publish-mode measurement: the publisher under test, its
// options and deadline, and the admission tally the feeder fleet fills in.
type sampleRun struct {
	pad      string
	pub      bus.Publisher
	opts     publishOpts
	deadline time.Time
	tally    struct {
		admitted       uint64
		errors         uint64
		confirmSkipped uint64
	}
}

// drive launches one feeder per pooled connection — each under its own publish
// partition — and returns their merged commit-latency samples.
func (r *sampleRun) drive(ctx context.Context) []int64 {
	feeders := max(r.opts.feeders, 1)
	samplesCh := make(chan []int64, feeders)
	var wg sync.WaitGroup
	for f := range feeders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samplesCh <- r.feeder(bus.WithPublishPartition(ctx, strconv.Itoa(f)), f)
		}()
	}
	wg.Wait()
	close(samplesCh)
	var samples []int64
	for s := range samplesCh {
		samples = append(samples, s...)
	}
	return samples
}

// message mints the next message for a feeder's sequence number, marking it for
// confirmation on every confirmEvery-th sequence.
func (r *sampleRun) message(seq uint64) benchMessage {
	globalSeq := uint64(r.opts.podIndex)<<48 | seq
	return benchMessage{
		subject:   r.opts.lane.subject,
		id:        fmt.Sprintf("bench-%d-%d", r.opts.podIndex, seq) + r.pad,
		body:      buildPayload(globalSeq, unixNano(time.Now().UnixNano()), r.opts.payloadSize),
		confirmed: r.opts.confirmEvery > 0 && seq%uint64(r.opts.confirmEvery) == 0,
	}
}

// count folds one publish result into the run's admission counters.
func (r *sampleRun) count(err error) {
	if err != nil {
		atomic.AddUint64(&r.tally.errors, 1)
		return
	}
	atomic.AddUint64(&r.tally.admitted, 1)
}

// confirmAsync publishes a sampled message on its own goroutine so the feeder
// returns to the pacer immediately. The caller must already hold lane's confirm
// slot; this releases it when the commit lands.
func (r *sampleRun) confirmAsync(ctx context.Context, lane *confirmLane, m benchMessage) {
	lane.wg.Add(1)
	go func() {
		defer lane.wg.Done()
		defer lane.release()
		elapsed, err := r.sendOne(ctx, m)
		r.count(err)
		if err == nil {
			lane.record(elapsed.Nanoseconds())
		}
	}()
}

// feeder publishes under one partition until the deadline. hashStreamRouter
// pins one routing key to ONE pooled connection, so an unpartitioned feeder
// engages exactly one worker of the pool and the other members never build a
// worker at all. One feeder per pooled connection is the minimum shape that
// exercises the whole publisher; ordering is per-feeder, which is all the
// latency samples need.
func (r *sampleRun) feeder(ctx context.Context, f int) []int64 {
	stride := max(r.opts.feeders, 1)
	pacer := newFeedPacer(r.opts.rate, stride, r.opts.paceEvery)
	lane := newConfirmLane()
	seq := uint64(f)
	for time.Now().Before(r.deadline) {
		pacer.wait()
		seq += uint64(stride)
		msg := r.message(seq)
		if msg.confirmed && lane.take() {
			r.confirmAsync(ctx, lane, msg)
			continue
		}
		if msg.confirmed {
			atomic.AddUint64(&r.tally.confirmSkipped, 1)
			msg.confirmed = false
		}
		_, err := r.sendOne(ctx, msg)
		r.count(err)
	}
	return lane.drain()
}

// avgCohort is the mean cohort size every publisher in this process sent.
func avgCohort() float64 {
	cohorts, messages := bus.CohortStats()
	if cohorts == 0 {
		return 0
	}
	return float64(messages) / float64(cohorts)
}
