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
	var admitted, pErrs uint64
	samples := collectFeedSamples(ctx, pub, o, deadline, &admitted, &pErrs)

	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = pub.Flush(flushCtx)
	cancel()

	elapsed := time.Since(windowStart)
	sortAsc(samples)
	emit(publishReport{
		Admitted:    admitted,
		Errors:      pErrs,
		ElapsedS:    elapsed.Seconds(),
		OfferedRate: float64(admitted) / elapsed.Seconds(),
		CommitNs:    summarize(samples),
	})
	return nil
}

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// their merged commit-latency samples.

// collectFeedSamples drives every feeder goroutine to its deadline and returns
// their merged commit-latency samples.
func collectFeedSamples(ctx context.Context, pub bus.Publisher, o publishOpts, deadline time.Time, admitted, pErrs *uint64) []int64 {
	feeders := max(o.feeders, 1)
	samplesCh := make(chan []int64, feeders)
	var wg sync.WaitGroup
	for f := range feeders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samplesCh <- runFeeder(bus.WithPublishPartition(ctx, strconv.Itoa(f)), pub, o, f, deadline, admitted, pErrs)
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

// feedPacer spaces a feeder's publishes so the pool offers rate/feeders msg/s
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

// publishOne sends one message, confirmed (commit latency sampled) or raw.
func publishOne(ctx context.Context, pub bus.Publisher, subject, id string, body []byte, confirmed bool) (time.Duration, error) {
	t0 := time.Now()
	var err error
	if confirmed {
		err = bus.PublishConfirmed(ctx, pub, bus.Publication{Subject: subject, ID: id, Payload: body})
	} else {
		err = bus.PublishRaw(ctx, pub, subject, body)
	}
	return time.Since(t0), err
}

// runFeeder publishes under one partition until deadline. hashStreamRouter pins
// one routing key to ONE pooled connection, so an unpartitioned feeder engages
// exactly one worker of the pool and the other members never build a worker at
// all. One feeder per pooled connection, each under its own publish partition,
// is the minimum shape that exercises the whole publisher; ordering is
// per-feeder, which is all the latency samples need.

// runFeeder publishes under one partition until deadline. hashStreamRouter pins
// one routing key to ONE pooled connection, so an unpartitioned feeder engages
// exactly one worker of the pool and the other members never build a worker at
// all. One feeder per pooled connection, each under its own publish partition,
// is the minimum shape that exercises the whole publisher; ordering is
// per-feeder, which is all the latency samples need.
func runFeeder(ctx context.Context, pub bus.Publisher, o publishOpts, f int, deadline time.Time, admitted, pErrs *uint64) []int64 {
	pacer := newFeedPacer(o.rate, max(o.feeders, 1))
	seq := uint64(f)
	var samples []int64
	for time.Now().Before(deadline) {
		pacer.wait()
		seq += uint64(max(o.feeders, 1))
		globalSeq := uint64(o.podIndex)<<48 | seq
		body := buildPayload(globalSeq, unixNano(time.Now().UnixNano()), o.payloadSize)
		confirmed := o.confirmEvery > 0 && seq%uint64(o.confirmEvery) == 0
		id := fmt.Sprintf("bench-%d-%d", o.podIndex, seq)
		elapsed, err := publishOne(ctx, pub, o.lane.subject, id, body, confirmed)
		if err != nil {
			atomic.AddUint64(pErrs, 1)
		} else {
			atomic.AddUint64(admitted, 1)
		}
		if confirmed {
			samples = append(samples, elapsed.Nanoseconds())
		}
	}
	return samples
}
