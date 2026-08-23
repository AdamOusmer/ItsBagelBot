// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"ItsBagelBot/pkg/bus"
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
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

	pub, err := bus.NewPublisher(o.lane.url, zap.NewNop())
	if err != nil {
		return err
	}
	defer pub.Close()

	ctx := context.Background()
	run := &sampleRun{pub: pub, opts: o, deadline: deadline}
	samples := run.drive(ctx)

	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = pub.Flush(flushCtx)
	cancel()

	elapsed := time.Since(windowStart)
	sortAsc(samples)
	emit(publishReport{
		Admitted:    run.tally.admitted,
		Errors:      run.tally.errors,
		ElapsedS:    elapsed.Seconds(),
		OfferedRate: float64(run.tally.admitted) / elapsed.Seconds(),
		CommitNs:    summarize(samples),
	})
	return nil
}

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// their merged commit-latency samples.

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// in aggregate; disabled when no rate was requested.

// feedPacer spaces a feeder's publishes so the pool offers rate/feeders msg/s
// in aggregate; disabled when no rate was requested.
type feedPacer struct {
	on     bool
	slot   time.Time
	stride time.Duration
}

func newFeedPacer(rate, feeders int) feedPacer {
	if rate <= 0 {
		return feedPacer{}
	}
	return feedPacer{on: true, slot: time.Now(), stride: time.Second * time.Duration(feeders) / time.Duration(rate)}
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

// sampleRun is one publish-mode measurement: the publisher under test, its
// options and deadline, and the admission tally the feeder fleet fills in.
type sampleRun struct {
	pub      bus.Publisher
	opts     publishOpts
	deadline time.Time
	tally    struct {
		admitted uint64
		errors   uint64
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

// feeder publishes under one partition until the deadline. hashStreamRouter
// pins one routing key to ONE pooled connection, so an unpartitioned feeder
// engages exactly one worker of the pool and the other members never build a
// worker at all. One feeder per pooled connection is the minimum shape that
// exercises the whole publisher; ordering is per-feeder, which is all the
// latency samples need.
func (r *sampleRun) feeder(ctx context.Context, f int) []int64 {
	pacer := newFeedPacer(r.opts.rate, max(r.opts.feeders, 1))
	seq := uint64(f)
	var samples []int64
	for time.Now().Before(r.deadline) {
		pacer.wait()
		seq += uint64(max(r.opts.feeders, 1))
		globalSeq := uint64(r.opts.podIndex)<<48 | seq
		body := buildPayload(globalSeq, unixNano(time.Now().UnixNano()), r.opts.payloadSize)
		msg := benchMessage{
			subject:   r.opts.lane.subject,
			id:        fmt.Sprintf("bench-%d-%d", r.opts.podIndex, seq),
			body:      body,
			confirmed: r.opts.confirmEvery > 0 && seq%uint64(r.opts.confirmEvery) == 0,
		}
		elapsed, err := r.sendOne(ctx, msg)
		if err != nil {
			atomic.AddUint64(&r.tally.errors, 1)
		} else {
			atomic.AddUint64(&r.tally.admitted, 1)
		}
		if msg.confirmed {
			samples = append(samples, elapsed.Nanoseconds())
		}
	}
	return samples
}
