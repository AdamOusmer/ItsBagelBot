// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"go.uber.org/zap"
)

type wireMode int

const (
	wireSingle wireMode = iota
	wireAtomic
	wireFast
)

const atomicBatchMax = 1000

const defaultAtomicPublishBatchSize = 256

const fastSessionMax = 65_536

func publishWireMode() wireMode {
	switch env.Get("NATS_PUBLISH_WIRE", "single") {
	case "atomic":
		return wireAtomic
	case "fast":
		return wireFast
	default:
		return wireSingle
	}
}

func cohortWire(wire wireMode, cohort int) wireMode {
	if cohort < 2 {
		return wireSingle
	}
	return wire
}

func publishBatchSize(wire wireMode) int {
	switch wire {
	case wireAtomic:
		return atomicPublishBatchSize()
	case wireFast:
		return fastPublishBatchSize()
	default:
		return defaultPublishBatchSize
	}
}

func publishBatchWait(wire wireMode) time.Duration {
	switch wire {
	case wireAtomic:
		return atomicPublishBatchWait()
	case wireFast:
		return fastPublishBatchWait()
	default:
		return defaultBatchWait()
	}
}

func defaultBatchWait() time.Duration {
	return max(env.GetDuration("NATS_PUBLISH_BATCH_WAIT", defaultPublishBatchWait), 0)
}

func atomicPublishBatchSize() int {
	size := env.GetInt("NATS_ATOMIC_PUBLISH_BATCH_SIZE", defaultAtomicPublishBatchSize)
	return min(max(size, 2), atomicBatchMax)
}

func atomicPublishBatchWait() time.Duration {
	wait := env.GetDuration("NATS_ATOMIC_PUBLISH_BATCH_WAIT", defaultPublishBatchWait)
	return min(max(wait, 100*time.Microsecond), 20*time.Millisecond)
}

func atomicPublishOverlap() bool {
	return env.GetBool("NATS_ATOMIC_PUBLISH_OVERLAP", true)
}

func fastPublishBatchSize() int {
	size := env.GetInt("NATS_FAST_PUBLISH_BATCH_SIZE", 8192)
	if size < 1 {
		return 1
	}
	return min(size, fastSessionMax)
}

func fastPublishBatchWait() time.Duration {
	wait := env.GetDuration("NATS_FAST_PUBLISH_BATCH_WAIT", 10*time.Millisecond)
	return min(max(wait, time.Millisecond), 100*time.Millisecond)
}

func fastPublishFlow(batchSize int, outstanding uint16) uint16 {
	flow := env.GetInt("NATS_FAST_PUBLISH_FLOW", 100)
	maxUseful := max((batchSize-1)/max(int(outstanding), 1), 1)
	bounded := min(max(flow, 1), maxUseful)
	if bounded > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(bounded)
}

func fastPublishOutstanding() uint16 {
	outstanding := max(env.GetInt("NATS_FAST_PUBLISH_OUTSTANDING_ACKS", 8), 1)
	if outstanding > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(outstanding)
}

type atomicCohortPublisher interface {
	AddMsg(*nats.Msg, ...jetstreamext.BatchMsgOpt) error
	CommitMsg(context.Context, *nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error)
	Discard() error
}

// Orbit's batch publisher is not safe for concurrent use; one goroutine owns a cohort.
type atomicCohort struct {
	publisher atomicCohortPublisher
	batch     []publishRequest
	stageErr  error
}

func (w *publishBatchWorker) publishAtomicCohort(batch []publishRequest, held bool) {
	if w.overlapCommit {
		w.publishAtomicOverlapped(batch, held)
		return
	}
	w.dropSlot(held)
	w.finish(batch, w.publishAtomic(batch))
}

func (w *publishBatchWorker) publishAtomicOverlapped(batch []publishRequest, held bool) {
	if err := atomicCohortFits(batch); err != nil {
		w.dropSlot(held)
		w.finish(batch, err)
		return
	}
	w.takeSlot(held)

	cohort := w.stageAtomic(batch)
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		w.finish(batch, w.resolveAtomic(cohort))
	}()
}

func (w *publishBatchWorker) publishAtomic(batch []publishRequest) error {
	if err := atomicCohortFits(batch); err != nil {
		return err
	}
	return w.resolveAtomic(w.stageAtomic(batch))
}

func atomicCohortFits(batch []publishRequest) error {
	if len(batch) < 1 || len(batch) > atomicBatchMax {
		return fmt.Errorf("bus: atomic cohort of %d messages is outside the 1..%d server range", len(batch), atomicBatchMax)
	}
	return nil
}

func (w *publishBatchWorker) stageAtomic(batch []publishRequest) atomicCohort {
	publisher, err := w.atomicPublisher()
	if err != nil {
		return atomicCohort{batch: batch, stageErr: err}
	}
	if err := addAtomicBatch(publisher, batch[:len(batch)-1]); err != nil {
		_ = publisher.Discard()
		return atomicCohort{batch: batch, stageErr: err}
	}
	return atomicCohort{publisher: publisher, batch: batch}
}

func (w *publishBatchWorker) resolveAtomic(cohort atomicCohort) error {
	if cohort.stageErr != nil {
		return w.atomicFallback(cohort.batch, cohort.stageErr)
	}
	last := len(cohort.batch) - 1
	return w.commitAtomic(cohort.publisher, cohort.batch, cohort.batch[last].msg)
}

func (w *publishBatchWorker) atomicPublisher() (atomicCohortPublisher, error) {
	if w.newAtomic != nil {
		return w.newAtomic()
	}
	publisher, err := jetstreamext.NewBatchPublisher(
		w.owner.modern,
		jetstreamext.BatchFlowControl{AckFirst: w.ackFirst, AckTimeout: publishAckWait()},
	)
	if err != nil {
		return nil, err
	}
	return publisher, nil
}

func addAtomicBatch(publisher atomicCohortPublisher, batch []publishRequest) error {
	for i := range batch {
		if err := publisher.AddMsg(batch[i].msg); err != nil {
			return err
		}
	}
	return nil
}

func (w *publishBatchWorker) commitAtomic(publisher atomicCohortPublisher, batch []publishRequest, commit *nats.Msg) error {
	ctx, cancel := context.WithTimeout(context.Background(), publishAckWait())
	ack, err := publisher.CommitMsg(ctx, commit)
	cancel()
	if err != nil {
		return w.atomicFallback(batch, err)
	}
	if ack == nil || ack.BatchSize != uint64(len(batch)) {
		return fmt.Errorf("bus: Orbit atomic commit stored %d/%d messages", orbitBatchSize(ack), len(batch))
	}
	return nil
}

func (w *publishBatchWorker) atomicFallback(batch []publishRequest, cause error) error {
	if !brokerRejectedBatch(cause) {
		return cause
	}
	w.owner.log.Warn("Orbit atomic batch rejected; re-publishing cohort individually",
		zap.Int("messages", len(batch)), zap.Error(cause))
	stripOrbitBatchFraming(batch)
	return w.publishCohortIndividually(batch)
}

// Only a typed broker rejection proves nothing was stored; replay nothing else.
func brokerRejectedBatch(cause error) bool {
	var apiErr *jsapi.APIError
	return errors.As(cause, &apiErr)
}

func orbitBatchSize(ack *jetstreamext.BatchAck) uint64 {
	if ack == nil {
		return 0
	}
	return ack.BatchSize
}

func stripOrbitBatchFraming(batch []publishRequest) {
	for i := range batch {
		msg := batch[i].msg
		msg.Header.Del(jetstreamext.BatchIDHeader)
		msg.Header.Del(jetstreamext.BatchSeqHeader)
		msg.Header.Del(jetstreamext.BatchCommitHeader)
		msg.Reply = ""
	}
}

type fastCohortPublisher interface {
	AddMsg(*nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.FastPubAck, error)
	CommitMsg(context.Context, *nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error)
	Close(context.Context) (*jetstreamext.BatchAck, error)
}

type fastCohortOutcome struct {
	acked         int
	streamSeq     uint64
	rejectedFirst bool
	err           error
}

func (w *publishBatchWorker) publishFast(batch []publishRequest) fastCohortOutcome {
	var asyncErr firstAsyncPublishError
	outstanding := fastPublishOutstanding()
	publisher, err := jetstreamext.NewFastPublisher(
		w.owner.modern,
		jetstreamext.FastPublishFlowControl{
			Flow:               fastPublishFlow(w.batchSize, outstanding),
			MaxOutstandingAcks: outstanding,
			AckTimeout:         publishAckWait(),
		},
		jetstreamext.WithFastPublisherErrorHandler(asyncErr.set),
	)
	if err != nil {
		return fastCohortOutcome{err: err}
	}
	outcome := publishFastCohort(publisher, batch, &asyncErr)
	if !outcome.rejectedFirst {
		return outcome
	}
	return w.fastFallback(batch, outcome.err)
}

func (w *publishBatchWorker) fastFallback(batch []publishRequest, cause error) fastCohortOutcome {
	w.owner.log.Warn("Orbit Fast-Ingest session rejected at its first message; re-publishing cohort individually",
		zap.Int("messages", len(batch)), zap.Error(cause))
	stripOrbitBatchFraming(batch)
	if err := w.publishCohortIndividually(batch); err != nil {
		return fastCohortOutcome{err: err}
	}
	return fastCohortOutcome{acked: len(batch)}
}

func (w *publishBatchWorker) finishFast(batch []publishRequest, outcome fastCohortOutcome) {
	stored := outcome.storedPrefix(len(batch))
	if stored == 0 {
		w.finish(batch, outcome.err)
		return
	}
	w.finish(batch[:stored], nil)
	if stored < len(batch) {
		w.finish(batch[stored:], outcome.err)
	}
}

func (o fastCohortOutcome) storedPrefix(size int) int {
	if o.err == nil {
		return size
	}
	return min(max(o.acked, 0), size-1)
}

func publishFastCohort(publisher fastCohortPublisher, batch []publishRequest, asyncErr *firstAsyncPublishError) fastCohortOutcome {
	var outcome fastCohortOutcome
	for i := 0; i < len(batch)-1; i++ {
		ack, err := publisher.AddMsg(batch[i].msg)
		outcome.observe(ack, len(batch))
		if cause := fastSessionCause(err, asyncErr); cause != nil {
			return outcome.abort(publisher, i, cause)
		}
	}
	return commitFastCohort(publisher, batch, asyncErr, outcome)
}

func fastSessionCause(stepErr error, asyncErr *firstAsyncPublishError) error {
	if reported := asyncErr.get(); reported != nil {
		return reported
	}
	return stepErr
}

func commitFastCohort(
	publisher fastCohortPublisher,
	batch []publishRequest,
	asyncErr *firstAsyncPublishError,
	outcome fastCohortOutcome,
) fastCohortOutcome {
	last := len(batch) - 1
	ctx, cancel := context.WithTimeout(context.Background(), publishAckWait())
	ack, err := publisher.CommitMsg(ctx, batch[last].msg)
	cancel()
	outcome.recordTerminal(ack)
	if cause := fastSessionCause(err, asyncErr); cause != nil {
		return outcome.abort(publisher, last, cause)
	}
	return outcome.settle(ack, len(batch))
}

func (o *fastCohortOutcome) observe(ack *jetstreamext.FastPubAck, size int) {
	if ack == nil || ack.AckSequence > uint64(size) {
		return
	}
	o.acked = max(o.acked, int(ack.AckSequence))
}

func (o *fastCohortOutcome) recordTerminal(ack *jetstreamext.BatchAck) {
	if ack == nil {
		return
	}
	o.streamSeq = ack.Sequence
}

func (o fastCohortOutcome) abort(publisher fastCohortPublisher, index int, cause error) fastCohortOutcome {
	o.recordTerminal(closeFastSession(publisher))
	o.err = cause
	o.rejectedFirst = index == 0 && o.acked == 0 && brokerRejectedBatch(cause)
	return o
}

func (o fastCohortOutcome) settle(ack *jetstreamext.BatchAck, size int) fastCohortOutcome {
	if ack != nil && ack.BatchSize == uint64(size) {
		o.acked = size
		return o
	}
	o.err = fmt.Errorf(
		"bus: Orbit Fast-Ingest session covered %d/%d batch sequences; %d messages acknowledged as stored, last stream sequence %d",
		orbitBatchSize(ack), size, o.acked, o.streamSeq)
	return o
}

func closeFastSession(publisher fastCohortPublisher) *jetstreamext.BatchAck {
	ctx, cancel := context.WithTimeout(context.Background(), publishAckWait())
	defer cancel()
	ack, _ := publisher.Close(ctx)
	return ack
}

type firstAsyncPublishError struct {
	mu  sync.Mutex
	err error
}

func (e *firstAsyncPublishError) set(err error) {
	e.mu.Lock()
	if e.err == nil {
		e.err = err
	}
	e.mu.Unlock()
}

func (e *firstAsyncPublishError) get() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}
