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

type publishOpts struct {
	lane         benchLane
	duration     time.Duration
	startAt      unixNano
	payloadSize  int
	confirmEvery int
	rate         int
	idPad        int
	paceEvery    int
	podIndex     int
	feeders      int
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

type benchMessage struct {
	subject   string
	id        string
	body      []byte
	confirmed bool
}

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

func (l *confirmLane) drain() []int64 {
	l.wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.samples
}

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

func (r *sampleRun) message(seq uint64) benchMessage {
	globalSeq := uint64(r.opts.podIndex)<<48 | seq
	return benchMessage{
		subject:   r.opts.lane.subject,
		id:        fmt.Sprintf("bench-%d-%d", r.opts.podIndex, seq) + r.pad,
		body:      buildPayload(globalSeq, unixNano(time.Now().UnixNano()), r.opts.payloadSize),
		confirmed: r.opts.confirmEvery > 0 && seq%uint64(r.opts.confirmEvery) == 0,
	}
}

func (r *sampleRun) count(err error) {
	if err != nil {
		atomic.AddUint64(&r.tally.errors, 1)
		return
	}
	atomic.AddUint64(&r.tally.admitted, 1)
}

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

func avgCohort() float64 {
	cohorts, messages := bus.CohortStats()
	if cohorts == 0 {
		return 0
	}
	return float64(messages) / float64(cohorts)
}
