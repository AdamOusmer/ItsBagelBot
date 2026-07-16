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

type messageSequence int

type publishRange struct {
	offset messageSequence
	size   int
}

type latencySampleIndex int

type percentileRank float64

func (j publishJob) payload(sequence messageSequence) []byte {
	index := (j.publisher*j.count + int(sequence)) % len(j.payloads)
	return j.payloads[index]
}

func (j publishJob) remaining(sequence messageSequence) int {
	return j.count - int(sequence)
}

func (j publishJob) last(sequence messageSequence) bool {
	return int(sequence) == j.count-1
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
		window := publishRange{offset: messageSequence(offset), size: size}
		batch, err := enqueueWindow(job, window)
		if err != nil {
			return err
		}
		if err := awaitWindow(job, batch); err != nil {
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
		batch := publishRange{offset: messageSequence(offset), size: size}
		go func(batch publishRange) {
			defer batches.Done()
			defer func() { <-semaphore }()
			if err := publishAtomicBatch(job, batch); err != nil {
				message := err.Error()
				firstErr.CompareAndSwap(nil, &message)
				failed.Store(true)
			}
		}(batch)
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

func publishAtomicBatch(job publishJob, batch publishRange) error {
	publisher, err := jetstreamext.NewBatchPublisher(job.client.modern, jetstreamext.BatchFlowControl{
		AckFirst: true, AckTimeout: job.cfg.ackTimeout,
	})
	if err != nil {
		job.counters.failures.Add(int64(batch.size))
		return err
	}
	for i := 0; i < batch.size; i++ {
		sequence := batch.offset + messageSequence(i)
		if err := pacePublish(job, sequence); err != nil {
			_ = publisher.Discard()
			return err
		}
		payload := job.payload(sequence)
		if i == batch.size-1 {
			commitCtx, cancel := context.WithTimeout(job.ctx, job.cfg.ackTimeout)
			_, err = publisher.Commit(commitCtx, job.cfg.subject, payload)
			cancel()
		} else {
			err = publisher.Add(job.cfg.subject, payload)
		}
		if err != nil {
			job.counters.failures.Add(int64(batch.size))
			recordTimeout(job.counters, err)
			_ = publisher.Discard()
			return fmt.Errorf("publisher %d atomic batch at %d: %w", job.publisher, batch.offset, err)
		}
	}
	job.counters.recordAcknowledged(job.publisher, int64(batch.size))
	return nil
}

func publishFast(job publishJob) error {
	session, err := newFastPublishSession(job)
	if err != nil {
		return err
	}
	for sequence := messageSequence(0); sequence < messageSequence(job.count); sequence++ {
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

func (s *fastPublishSession) publish(sequence messageSequence) error {
	if err := pacePublish(s.job, sequence); err != nil {
		return err
	}
	if err := s.send(sequence); err != nil {
		return s.failSequence(sequence, err)
	}
	return s.failOnAsyncError(sequence)
}

func (s *fastPublishSession) send(sequence messageSequence) error {
	payload := s.job.payload(sequence)
	if !s.job.last(sequence) {
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

func (s *fastPublishSession) failSequence(sequence messageSequence, err error) error {
	s.job.counters.failures.Add(int64(s.job.remaining(sequence)))
	recordTimeout(s.job.counters, err)
	return fmt.Errorf("publisher %d fast sequence %d: %w", s.job.publisher, sequence, err)
}

func (s *fastPublishSession) failOnAsyncError(sequence messageSequence) error {
	message := s.asyncErr.Load()
	if message == nil {
		return nil
	}
	s.job.counters.failures.Add(int64(s.job.remaining(sequence)))
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
	s.job.counters.recordAcknowledged(s.job.publisher, int64(s.job.count))
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

func pacePublish(job publishJob, sequence messageSequence) error {
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
	ok       <-chan *nats.PubAck
	err      <-chan error
	sequence messageSequence
}

const ackProgressPollInterval = 64

func enqueueWindow(job publishJob, window publishRange) ([]pendingPublish, error) {
	batch := make([]pendingPublish, 0, window.size)
	for i := 0; i < window.size; i++ {
		sequence := window.offset + messageSequence(i)
		if err := pacePublish(job, sequence); err != nil {
			return nil, err
		}
		msg := benchmarkMessage(job.cfg.subject, job.payload(sequence))
		future, err := job.client.js.PublishMsgAsync(msg)
		if err != nil {
			job.counters.failures.Add(1)
			return nil, fmt.Errorf("publisher %d sequence %d enqueue: %w", job.publisher, sequence, err)
		}
		batch = append(batch, pendingPublish{
			ok: future.Ok(), err: future.Err(), sequence: sequence,
		})
		if (i+1)%ackProgressPollInterval == 0 {
			batch, err = drainReadyAcknowledgements(job, batch)
			if err != nil {
				return nil, err
			}
		}
	}
	return batch, nil
}

// drainReadyAcknowledgements observes completed futures while the next async
// window is still being filled. This preserves the large throughput window
// without making a paced low-rate run look stalled until all 16K entries have
// been enqueued. It is allocation-free and walks only the completed prefix.
func drainReadyAcknowledgements(job publishJob, batch []pendingPublish) ([]pendingPublish, error) {
	for len(batch) > 0 {
		item := batch[0]
		select {
		case <-item.ok:
			job.counters.recordAcknowledged(job.publisher, 1)
			batch = batch[1:]
		case err := <-item.err:
			job.counters.failures.Add(int64(len(batch)))
			recordTimeout(job.counters, err)
			return nil, fmt.Errorf("publisher %d PubAck %d: %w", job.publisher, item.sequence, err)
		default:
			return batch, nil
		}
	}
	return batch, nil
}

func awaitWindow(job publishJob, batch []pendingPublish) error {
	deadline := time.NewTimer(job.cfg.ackTimeout)
	defer stopAndDrain(deadline)
	for i, item := range batch {
		if err := awaitFuture(job.ctx, item.ok, item.err, deadline.C); err != nil {
			job.counters.failures.Add(int64(len(batch) - i))
			recordTimeout(job.counters, err)
			return fmt.Errorf("publisher %d PubAck %d: %w", job.publisher, item.sequence, err)
		}
		job.counters.recordAcknowledged(job.publisher, 1)
	}
	return nil
}

func awaitFuture(
	ctx context.Context,
	ok <-chan *nats.PubAck,
	errCh <-chan error,
	timeout <-chan time.Time,
) error {
	select {
	case <-ok:
		return nil
	case err := <-errCh:
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
	clients  []client
	payloads [][]byte
	result   latencyResult
}

func latencyProbe(
	ctx context.Context,
	cfg config,
	clients []client,
	payloads [][]byte,
) latencyResult {
	sampler := latencySampler{
		ctx: ctx, cfg: cfg, clients: clients, payloads: payloads,
		result: latencyResult{values: make([]time.Duration, 0, cfg.latencySamples)},
	}
	sampler.collect()
	return sampler.result
}

func (s *latencySampler) collect() {
	for index := latencySampleIndex(0); index < latencySampleIndex(s.cfg.latencySamples); index++ {
		if !s.collectSample(index) {
			return
		}
	}
}

func (s *latencySampler) collectSample(index latencySampleIndex) bool {
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

func (s *latencySampler) ready(index latencySampleIndex) bool {
	if index == 0 {
		return true
	}
	return waitForSample(s.ctx, s.cfg.latencyInterval)
}

func (s *latencySampler) measure(index latencySampleIndex) (time.Duration, error) {
	msg := benchmarkMessage(s.cfg.subject, s.payloads[int(index)%len(s.payloads)])
	js := s.clients[int(index)%len(s.clients)].js
	started := time.Now()
	sampleCtx, cancel := context.WithTimeout(s.ctx, s.cfg.ackTimeout)
	defer cancel()
	_, err := js.PublishMsg(msg, nats.Context(sampleCtx))
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

func percentile(values []time.Duration, rank percentileRank) time.Duration {
	index := int(math.Ceil(float64(len(values))*float64(rank))) - 1
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
