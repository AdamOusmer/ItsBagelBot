package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/jetstreamext"
)

type publishJob struct {
	ctx       context.Context
	cfg       config
	client    client
	publisher int
	count     int
	payloads  [][]byte
	counters  *benchmarkCounters
	started   time.Time
}

func publishByMode(job publishJob) error {
	if err := job.ctx.Err(); err != nil {
		return err
	}
	switch job.cfg.mode {
	case "atomic":
		return publishAtomicBatches(job)
	case "fast":
		return publishFast(job)
	default:
		return publishAsyncWindows(job)
	}
}

func publishAsyncWindows(job publishJob) error {
	for offset := 0; offset < job.count; offset += job.cfg.window {
		size := min(job.cfg.window, job.count-offset)
		batch, err := enqueueWindow(job, offset, size)
		if err != nil {
			return err
		}
		if err := awaitWindow(job, offset, batch); err != nil {
			return err
		}
	}
	return nil
}

func publishAtomicBatches(job publishJob) error {
	semaphore := make(chan struct{}, job.cfg.atomicInflight)
	var batches sync.WaitGroup
	var firstErr atomic.Pointer[string]
	var failed atomic.Bool
	for offset := 0; offset < job.count; offset += job.cfg.batchSize {
		if failed.Load() || job.ctx.Err() != nil {
			break
		}
		size := min(job.cfg.batchSize, job.count-offset)
		semaphore <- struct{}{}
		// A failed batch may have released the slot that unblocked this send.
		// Recheck before launching so failure work stays bounded by the batches
		// that were already in flight.
		if failed.Load() {
			<-semaphore
			break
		}
		batches.Add(1)
		go func(offset, size int) {
			defer batches.Done()
			defer func() { <-semaphore }()
			if err := publishAtomicBatch(job, offset, size); err != nil {
				message := err.Error()
				firstErr.CompareAndSwap(nil, &message)
				failed.Store(true)
			}
		}(offset, size)
	}
	batches.Wait()
	if message := firstErr.Load(); message != nil {
		return errors.New(*message)
	}
	if err := job.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func publishAtomicBatch(job publishJob, offset, size int) error {
	publisher, err := jetstreamext.NewBatchPublisher(job.client.modern, jetstreamext.BatchFlowControl{
		AckFirst: true, AckTimeout: job.cfg.ackTimeout,
	})
	if err != nil {
		job.counters.failures.Add(int64(size))
		return err
	}
	for i := 0; i < size; i++ {
		sequence := offset + i
		if err := pacePublish(job, sequence); err != nil {
			_ = publisher.Discard()
			return err
		}
		payload := job.payloads[(job.publisher*job.count+sequence)%len(job.payloads)]
		if i == size-1 {
			commitCtx, cancel := context.WithTimeout(job.ctx, job.cfg.ackTimeout)
			_, err = publisher.Commit(commitCtx, job.cfg.subject, payload)
			cancel()
		} else {
			err = publisher.Add(job.cfg.subject, payload)
		}
		if err != nil {
			job.counters.failures.Add(int64(size))
			recordTimeout(job.counters, err)
			_ = publisher.Discard()
			return fmt.Errorf("publisher %d atomic batch at %d: %w", job.publisher, offset, err)
		}
	}
	job.counters.acked.Add(int64(size))
	return nil
}

func publishFast(job publishJob) error {
	session, err := newFastPublishSession(job)
	if err != nil {
		return err
	}
	for sequence := 0; sequence < job.count; sequence++ {
		if err := session.publish(sequence); err != nil {
			return err
		}
	}
	return session.finish()
}

type fastPublishSession struct {
	job       publishJob
	publisher jetstreamext.FastPublisher
	asyncErr  atomic.Pointer[string]
	committed uint64
}

func newFastPublishSession(job publishJob) (*fastPublishSession, error) {
	session := &fastPublishSession{job: job}
	publisher, err := jetstreamext.NewFastPublisher(
		job.client.modern,
		jetstreamext.FastPublishFlowControl{
			Flow: uint16(job.cfg.batchSize), MaxOutstandingAcks: uint16(job.cfg.fastOutstanding),
			AckTimeout: job.cfg.ackTimeout,
		},
		jetstreamext.WithFastPublisherErrorHandler(session.recordAsyncError),
	)
	if err != nil {
		job.counters.failures.Add(int64(job.count))
		return nil, err
	}
	session.publisher = publisher
	return session, nil
}

func (s *fastPublishSession) recordAsyncError(err error) {
	message := err.Error()
	s.asyncErr.CompareAndSwap(nil, &message)
}

func (s *fastPublishSession) publish(sequence int) error {
	if err := pacePublish(s.job, sequence); err != nil {
		return err
	}
	if err := s.send(sequence); err != nil {
		return s.failSequence(sequence, err)
	}
	return s.failOnAsyncError(sequence)
}

func (s *fastPublishSession) send(sequence int) error {
	payload := s.job.payloads[(s.job.publisher*s.job.count+sequence)%len(s.job.payloads)]
	if sequence != s.job.count-1 {
		_, err := s.publisher.Add(s.job.cfg.subject, payload)
		return err
	}
	return s.commit(payload)
}

func (s *fastPublishSession) commit(payload []byte) error {
	commitCtx, cancel := context.WithTimeout(s.job.ctx, s.job.cfg.ackTimeout)
	defer cancel()
	ack, err := s.publisher.Commit(commitCtx, s.job.cfg.subject, payload)
	if ack != nil {
		s.committed = ack.BatchSize
	}
	return err
}

func (s *fastPublishSession) failSequence(sequence int, err error) error {
	s.job.counters.failures.Add(int64(s.job.count - sequence))
	recordTimeout(s.job.counters, err)
	return fmt.Errorf("publisher %d fast sequence %d: %w", s.job.publisher, sequence, err)
}

func (s *fastPublishSession) failOnAsyncError(sequence int) error {
	message := s.asyncErr.Load()
	if message == nil {
		return nil
	}
	s.job.counters.failures.Add(int64(s.job.count - sequence))
	return errors.New(*message)
}

func (s *fastPublishSession) finish() error {
	if message := s.asyncErr.Load(); message != nil {
		s.job.counters.failures.Add(int64(s.job.count))
		return errors.New(*message)
	}
	if s.committed != uint64(s.job.count) {
		return s.failCommitSize()
	}
	s.job.counters.acked.Add(int64(s.job.count))
	return nil
}

func (s *fastPublishSession) failCommitSize() error {
	missing := s.job.count
	if s.committed < uint64(s.job.count) {
		missing -= int(s.committed)
	}
	s.job.counters.failures.Add(int64(missing))
	return fmt.Errorf(
		"publisher %d fast commit acknowledged %d/%d messages",
		s.job.publisher,
		s.committed,
		s.job.count,
	)
}

func pacePublish(job publishJob, sequence int) error {
	if err := job.ctx.Err(); err != nil {
		return err
	}
	if job.cfg.targetRate <= 0 {
		return nil
	}
	perPublisher := job.cfg.targetRate / float64(job.cfg.publishers)
	target := job.started.Add(time.Duration(float64(sequence) / perPublisher * float64(time.Second)))
	if wait := time.Until(target); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-job.ctx.Done():
			return job.ctx.Err()
		}
	}
	return nil
}

type pendingPublish struct {
	future nats.PubAckFuture
}

func enqueueWindow(job publishJob, offset, size int) ([]pendingPublish, error) {
	batch := make([]pendingPublish, 0, size)
	for i := 0; i < size; i++ {
		sequence := offset + i
		if err := pacePublish(job, sequence); err != nil {
			return nil, err
		}
		msg := benchmarkMessage(
			job.cfg.subject,
			job.payloads[(job.publisher*job.count+sequence)%len(job.payloads)],
		)
		future, err := job.client.js.PublishMsgAsync(msg)
		if err != nil {
			job.counters.failures.Add(1)
			return nil, fmt.Errorf("publisher %d sequence %d enqueue: %w", job.publisher, sequence, err)
		}
		batch = append(batch, pendingPublish{future: future})
	}
	return batch, nil
}

func awaitWindow(job publishJob, offset int, batch []pendingPublish) error {
	deadline := time.NewTimer(job.cfg.ackTimeout)
	defer stopAndDrain(deadline)
	for i, item := range batch {
		if err := awaitFuture(job.ctx, item.future, deadline.C); err != nil {
			job.counters.failures.Add(int64(len(batch) - i))
			recordTimeout(job.counters, err)
			return fmt.Errorf("publisher %d PubAck %d: %w", job.publisher, offset+i, err)
		}
		job.counters.acked.Add(1)
	}
	return nil
}

func awaitFuture(ctx context.Context, future nats.PubAckFuture, timeout <-chan time.Time) error {
	select {
	case <-future.Ok():
		return nil
	case err := <-future.Err():
		return err
	case <-timeout:
		return nats.ErrTimeout
	case <-ctx.Done():
		return ctx.Err()
	}
}

func recordTimeout(counters *benchmarkCounters, err error) {
	if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		counters.timeouts.Add(1)
	}
}

func stopAndDrain(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

type latencyResult struct {
	values   []time.Duration
	errors   int64
	timeouts int64
}

type latencySampler struct {
	ctx      context.Context
	cfg      config
	js       nats.JetStreamContext
	payloads [][]byte
	result   latencyResult
}

func latencyProbe(
	ctx context.Context,
	cfg config,
	js nats.JetStreamContext,
	payloads [][]byte,
) latencyResult {
	sampler := latencySampler{
		ctx: ctx, cfg: cfg, js: js, payloads: payloads,
		result: latencyResult{values: make([]time.Duration, 0, cfg.latencySamples)},
	}
	sampler.collect()
	return sampler.result
}

func (s *latencySampler) collect() {
	for index := 0; index < s.cfg.latencySamples; index++ {
		if !s.collectSample(index) {
			return
		}
	}
}

func (s *latencySampler) collectSample(index int) bool {
	if !s.ready(index) {
		return false
	}
	duration, err := s.measure(index)
	if err != nil {
		return s.recordError(err)
	}
	s.result.values = append(s.result.values, duration)
	return true
}

func (s *latencySampler) ready(index int) bool {
	if index == 0 {
		return true
	}
	return waitForSample(s.ctx, s.cfg.latencyInterval)
}

func (s *latencySampler) measure(index int) (time.Duration, error) {
	msg := benchmarkMessage(s.cfg.subject, s.payloads[index%len(s.payloads)])
	started := time.Now()
	sampleCtx, cancel := context.WithTimeout(s.ctx, s.cfg.ackTimeout)
	defer cancel()
	_, err := s.js.PublishMsg(msg, nats.Context(sampleCtx))
	return time.Since(started), err
}

func (s *latencySampler) recordError(err error) bool {
	if s.ctx.Err() != nil {
		return false
	}
	s.result.errors++
	if latencyTimeout(err) {
		s.result.timeouts++
	}
	return false
}

func latencyTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout)
}

// latencySampleRequirement prevents a healthy-looking percentile from being
// calculated from only a handful of replies during a rate-controlled soak.
// A sample can legitimately occupy both its configured interval and the p99
// budget, so the gate uses that conservative cadence and allows 20% jitter.
// Unpaced calibration runs have no known duration and keep the legacy
// one-sample minimum.
func latencySampleRequirement(cfg config) int {
	if cfg.latencySamples == 0 {
		return 0
	}
	if cfg.targetRate <= 0 {
		return 1
	}
	expectedLoad := time.Duration(float64(cfg.messages) / cfg.targetRate * float64(time.Second))
	sampleSlot := cfg.latencyInterval + cfg.maxP99
	possible := min(cfg.latencySamples, int(expectedLoad/sampleSlot))
	return max(1, possible*4/5)
}

func waitForSample(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func benchmarkMessage(subject string, payload []byte) *nats.Msg {
	return &nats.Msg{Subject: subject, Data: payload}
}

func percentile(values []time.Duration, p float64) time.Duration {
	index := int(math.Ceil(float64(len(values))*p)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func durationMS(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func tlsVersion(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("0x%x", version)
	}
}

func closeClients(clients []client) {
	for _, c := range clients {
		c.nc.Close()
	}
}
