// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
)

func TestAtomicFallbackRequiresExplicitBrokerRejection(t *testing.T) {
	rejected := &jsapi.APIError{Code: 400, ErrorCode: 10174, Description: "batch publish not enabled"}
	if !brokerRejectedBatch(rejected) {
		t.Fatal("a typed API rejection must permit the per-message fallback")
	}
	if !brokerRejectedBatch(fmt.Errorf("orbit: %w", rejected)) {
		t.Fatal("a wrapped API rejection must permit the per-message fallback")
	}
	if !brokerRejectedBatch(jetstreamext.ErrBatchPublishNotEnabled) {
		t.Fatal("a server feature rejection must permit the per-message fallback")
	}
	for name, err := range map[string]error{
		"transport":       errors.New("nats: connection closed"),
		"timeout":         context.DeadlineExceeded,
		"short ack":       errors.New("bus: Orbit atomic commit stored 2/3 messages"),
		"undecodable ack": jetstreamext.ErrInvalidBatchAck,
		"closed batch":    jetstreamext.ErrBatchClosed,
	} {
		if brokerRejectedBatch(err) {
			t.Fatalf("%s ambiguity must not permit replay: %v", name, err)
		}
	}
}

func TestAtomicFallbackDoesNotReplayAmbiguousFailure(t *testing.T) {
	// A worker with no connection state proves the ambiguous path never reaches
	// the re-publish machinery.
	worker := &publishBatchWorker{}
	want := errors.New("nats: connection closed")
	if err := worker.atomicFallback(stagedBatch(3), want); !errors.Is(err, want) {
		t.Fatalf("atomicFallback() error = %v, want the original cause", err)
	}
}

func TestAtomicPublisherRejectsOversizeCohortClientSide(t *testing.T) {
	worker := &publishBatchWorker{}
	if err := worker.publishAtomic(stagedBatch(atomicBatchMax + 1)); err == nil {
		t.Fatal("oversize atomic cohort reached Orbit")
	}
	if err := worker.publishAtomic(nil); err == nil {
		t.Fatal("empty atomic cohort reached Orbit")
	}
}

// Orbit's commit is a publish: the cohort's last message rides CommitMsg. The
// worker therefore stages exactly the first N-1 messages, in order, and the
// commit carries the last one.
func TestAtomicCohortStagesEveryMessageButTheCommitPayload(t *testing.T) {
	publisher := &stubAtomicPublisher{}
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) { return publisher, nil })
	batch := confirmedBatch(4)

	worker.publishAtomicOverlapped(batch)
	worker.acks.Wait()

	requireCohortConfirmed(t, batch, "a committed cohort")
	if len(publisher.added) != 3 {
		t.Fatalf("staged %d messages, want the cohort minus its commit payload", len(publisher.added))
	}
	for i, msg := range publisher.added {
		if got := msg.Header.Get(messageIDHeader); got != "id-"+itoa(i+1) {
			t.Fatalf("staged message %d is %s, want the cohort in wire order", i, got)
		}
	}
	if publisher.committed != batch[3].msg {
		t.Fatal("the commit did not carry the cohort's last message")
	}
}

// The overlap's whole point is that a cohort no longer waits behind an older
// cohort's commit ack. Each cohort resolves on its own commit, and its confirmed
// callers hear about that commit and no other.
func TestAtomicOverlapResolvesEachCohortOnItsOwnCommit(t *testing.T) {
	slow, opened := make(chan struct{}), 0
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) {
		opened++
		if opened == 1 {
			return &stubAtomicPublisher{release: slow}, nil
		}
		return &stubAtomicPublisher{}, nil
	})
	first, second := confirmedBatch(3), confirmedBatch(2)

	worker.publishAtomicOverlapped(first)
	worker.publishAtomicOverlapped(second)

	requireCohortConfirmed(t, second, "the second cohort")
	select {
	case err := <-first[0].confirmed:
		t.Fatalf("the first cohort resolved before its own commit: %v", err)
	default:
	}

	close(slow)
	worker.acks.Wait()
	requireCohortConfirmed(t, first, "the first cohort")
	if completed := worker.owner.completed; completed != 5 {
		t.Fatalf("resolved %d/5 messages", completed)
	}
}

// The overlap moves the ack wait, never the at-most-once rule: an ambiguous
// commit failure fails the cohort where it stands. The worker has no JetStream
// context that can publish, so a replay would be visible as a refused send.
func TestAtomicOverlapDoesNotReplayAnAmbiguousCommitFailure(t *testing.T) {
	want := errors.New("nats: connection closed")
	replay := &stubJetStream{err: errors.New("the per-message wire must not be used")}
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) {
		return &stubAtomicPublisher{commitErr: want}, nil
	})
	worker.js = replay
	batch := confirmedBatch(3)

	worker.publishAtomicOverlapped(batch)
	worker.acks.Wait()

	requireCohortFailedWith(t, batch, want, "ambiguous")
	if replay.published != 0 {
		t.Fatalf("an ambiguous commit failure re-published %d messages", replay.published)
	}
}

// A typed broker rejection is the one commit failure that proves nothing was
// stored, and the replay it authorises belongs to the goroutine that resolved
// the cohort, not to the worker.
func TestAtomicOverlapReplaysADefiniteBrokerRejection(t *testing.T) {
	rejected := &jsapi.APIError{Code: 400, ErrorCode: 10174, Description: "batch publish not enabled"}
	replay := &stubJetStream{err: errors.New("per-message wire refused")}
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) {
		return &stubAtomicPublisher{commitErr: rejected}, nil
	})
	worker.js = replay
	batch := confirmedBatch(3)

	worker.publishAtomicOverlapped(batch)
	worker.acks.Wait()

	if replay.published == 0 {
		t.Fatal("a typed broker rejection must reach the per-message wire")
	}
	for i := range batch {
		if err := <-batch[i].confirmed; err == nil {
			t.Fatalf("message %d passed although its replay was refused", i)
		}
	}
}

// A cohort Orbit refuses to stage never reaches a commit, so its verdict is
// settled by the same rules on the same goroutine.
func TestAtomicOverlapResolvesAStagingFailureWithoutCommitting(t *testing.T) {
	want := errors.New("nats: connection closed")
	publisher := &stubAtomicPublisher{addErr: want}
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) { return publisher, nil })
	worker.js = &stubJetStream{err: errors.New("the per-message wire must not be used")}
	batch := confirmedBatch(3)

	worker.publishAtomicOverlapped(batch)
	worker.acks.Wait()

	requireCohortFailedWith(t, batch, want, "staging")
	if publisher.committed != nil {
		t.Fatal("a cohort that failed to stage was committed")
	}
	if publisher.discards != 1 {
		t.Fatalf("a half-staged batch was discarded %d times, want exactly 1", publisher.discards)
	}
}

// The slots are the client's share of the broker's cap on concurrently staged
// batches per stream, so a worker must stall on staging rather than open a fifth
// batch.
func TestAtomicOverlapSlotsBoundTheCommitDepth(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{}, 8)
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) {
		return &stubAtomicPublisher{commitStarted: started, release: release}, nil
	})
	worker.slots = make(chan struct{}, 2)

	worker.publishAtomicOverlapped(confirmedBatch(2))
	worker.publishAtomicOverlapped(confirmedBatch(2))
	awaitSignal(t, started, "the first two commits never started")
	awaitSignal(t, started, "the second commit never started")

	staged := make(chan struct{})
	go func() {
		worker.publishAtomicOverlapped(confirmedBatch(2))
		close(staged)
	}()
	select {
	case <-staged:
		t.Fatal("a third cohort staged while both commit slots were held")
	case <-started:
		t.Fatal("a third commit started while both commit slots were held")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	awaitSignal(t, staged, "the blocked cohort never staged after a slot was freed")
	worker.acks.Wait()
}

// Stopping the worker must not abandon a cohort whose commit is still out: its
// callers are waiting on that verdict and the connection is about to drain.
func TestAtomicOverlapDrainsInFlightCommitsOnStop(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{}, 4)
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) {
		return &stubAtomicPublisher{commitStarted: started, release: release}, nil
	})
	worker.batchSize = 2
	go worker.run()

	batch := confirmedBatch(2)
	worker.requests <- batch[0]
	worker.requests <- batch[1]
	awaitSignal(t, started, "the cohort never reached its commit")

	close(worker.stop)
	select {
	case <-worker.done:
		t.Fatal("the worker finished while a commit was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	awaitSignal(t, worker.done, "the worker did not finish once its commit resolved")
	requireCohortConfirmed(t, batch, "a drained cohort")
}

// With the overlap off the commit stays on the calling goroutine, which is what
// keeps whole cohorts in stream order: the broker sequences an atomic batch at
// commit, not on arrival.
func TestAtomicCohortWithoutOverlapCommitsOnTheWorker(t *testing.T) {
	publisher := &stubAtomicPublisher{}
	worker := newOverlapWorker(func() (atomicCohortPublisher, error) { return publisher, nil })
	worker.overlapCommit = false
	batch := confirmedBatch(3)

	worker.publishAtomicCohort(batch)

	if publisher.committed == nil {
		t.Fatal("the cohort returned to the worker before its commit")
	}
	for i := range batch {
		requireResolvedNow(t, i, batch[i].confirmed)
	}
}

// requireCohortConfirmed states that every caller of a committed cohort got a
// nil verdict.
func requireCohortConfirmed(t *testing.T, batch []publishRequest, cohort string) {
	t.Helper()
	for i := range batch {
		if err := <-batch[i].confirmed; err != nil {
			t.Fatalf("message %d of %s failed: %v", i, cohort, err)
		}
	}
}

// requireCohortFailedWith states that every caller of a failed cohort got the
// one cause, and no caller got a verdict of its own.
func requireCohortFailedWith(t *testing.T, batch []publishRequest, want error, cause string) {
	t.Helper()
	for i := range batch {
		if err := <-batch[i].confirmed; !errors.Is(err, want) {
			t.Fatalf("message %d = %v, want the %s cause %v", i, err, cause, want)
		}
	}
}

// requireResolvedNow states that the verdict is already there: the worker
// returned only after resolving it, so a blocking read would be the bug.
func requireResolvedNow(t *testing.T, i int, confirmed <-chan error) {
	t.Helper()
	select {
	case err := <-confirmed:
		if err != nil {
			t.Fatalf("message %d failed: %v", i, err)
		}
	default:
		t.Fatalf("message %d was not resolved by the time the worker returned", i)
	}
}

// newOverlapWorker builds the worker state an atomic cohort touches, with the
// Orbit session replaced by a stub. Cohorts are handed to it directly, so the
// stub is only ever reached from the goroutine that owns the cohort.
func newOverlapWorker(newAtomic func() (atomicCohortPublisher, error)) *publishBatchWorker {
	owner := newTestBatchPublisher()
	owner.wire = wireAtomic
	return &publishBatchWorker{
		owner:         owner,
		requests:      make(chan publishRequest, 8),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		slots:         make(chan struct{}, 4),
		batchSize:     defaultAtomicPublishBatchSize,
		batchWait:     defaultPublishBatchWait,
		overlapCommit: true,
		newAtomic:     newAtomic,
	}
}

// stubAtomicPublisher stands in for Orbit's BatchPublisher. release holds the
// commit open so a cohort can be observed while its ack is still outstanding,
// which is the state the overlap exists to create.
type stubAtomicPublisher struct {
	added         []*nats.Msg
	committed     *nats.Msg
	addErr        error
	commitErr     error
	discards      int
	commitStarted chan struct{}
	release       chan struct{}
}

func (p *stubAtomicPublisher) AddMsg(msg *nats.Msg, _ ...jetstreamext.BatchMsgOpt) error {
	if p.addErr != nil {
		return p.addErr
	}
	p.added = append(p.added, msg)
	return nil
}

func (p *stubAtomicPublisher) CommitMsg(ctx context.Context, msg *nats.Msg, _ ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error) {
	p.committed = msg
	if p.commitStarted != nil {
		p.commitStarted <- struct{}{}
	}
	if err := p.awaitRelease(ctx); err != nil {
		return nil, err
	}
	if p.commitErr != nil {
		return nil, p.commitErr
	}
	// The ordinary broker answer: every staged message plus the commit payload
	// was stored.
	return &jetstreamext.BatchAck{BatchSize: uint64(len(p.added) + 1), Sequence: 42}, nil
}

func (p *stubAtomicPublisher) awaitRelease(ctx context.Context) error {
	if p.release == nil {
		return nil
	}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *stubAtomicPublisher) Discard() error {
	p.discards++
	return nil
}

// stubJetStream stands in for the per-message wire so a fallback replay is
// observable without a broker. Only PublishMsgAsync is reachable from the
// fallback; the embedded interface is nil and every other method would panic,
// which is the point. err must be set: a nil PubAckFuture is not awaitable.
type stubJetStream struct {
	nats.JetStreamContext
	published int
	err       error
}

func (s *stubJetStream) PublishMsgAsync(*nats.Msg, ...nats.PubOpt) (nats.PubAckFuture, error) {
	s.published++
	return nil, s.err
}
