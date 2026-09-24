// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func (p *batchPublisher) complete(count int, err error) {
	sequences := make([]uint64, count)
	for i := range sequences {
		sequences[i] = p.completed + uint64(i+1)
	}
	p.completeSequences(sequences, err)
}

func (p *batchPublisher) takeWindowErrLocked(target uint64) error {
	for i, failure := range p.errors {
		if failure.first > target {
			break
		}
		if failure.last > target {
			p.errors[i].first = target + 1
			return failure.cause
		}
		p.errors = append(p.errors[:i], p.errors[i+1:]...)
		return failure.cause
	}
	return nil
}

func TestPublishPartitionIsScopedAndStable(t *testing.T) {
	base := context.Background()
	if got := publishPartition(base); got != "" {
		t.Fatalf("base partition = %q, want empty", got)
	}
	ctx := WithPublishPartition(base, "channel-123")
	if got := publishPartition(ctx); got != "channel-123" {
		t.Fatalf("partition = %q, want channel-123", got)
	}
	router := hashStreamRouter{}
	first := router.Connection("TWITCH_OUTGRESS\x00"+publishPartition(ctx), 4)
	second := router.Connection("TWITCH_OUTGRESS\x00"+publishPartition(ctx), 4)
	if first != second {
		t.Fatalf("same aggregate moved connections: %d != %d", first, second)
	}
}

func TestPublishMessageUsesFleetIdentityWithoutBrokerDedup(t *testing.T) {
	msg := publishMessage(publishCommand{
		ctx: context.Background(), topic: "data.test", msgID: "event-42", payload: []byte("{}"),
	})

	if got := msg.Header.Get(messageIDHeader); got != "event-42" {
		t.Fatalf("fleet message id = %q, want event-42", got)
	}
	if got := msg.Header.Get(nats.MsgIdHdr); got != "" {
		t.Fatalf("broker dedup header unexpectedly set to %q", got)
	}
}

func TestFlushReportsAWindowFailureOnceInsteadOfLatchingIt(t *testing.T) {
	pub := newTestBatchPublisher()
	want := errors.New("cohort aborted")
	pub.markAccepted()
	pub.complete(1, want)

	if err := pub.Flush(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Flush() = %v, want the failed cohort", err)
	}

	pub.markAccepted()
	pub.complete(1, nil)
	if err := pub.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() after a healthy window = %v, want nil", err)
	}
}

func TestFlushWindowLeavesALaterCohortsFailureInPlace(t *testing.T) {
	pub := newTestBatchPublisher()
	want := errors.New("later cohort aborted")
	pub.errors = []publishError{{first: 9, last: 9, cause: want, count: 1}}

	if err := pub.takeWindowErrLocked(8); err != nil {
		t.Fatalf("window ending at 8 reported a failure that started at 8: %v", err)
	}
	if err := pub.takeWindowErrLocked(9); !errors.Is(err, want) {
		t.Fatalf("overlapping window = %v, want the failed cohort", err)
	}
	if len(pub.errors) != 0 {
		t.Fatal("a reported failure must not stay pending for the next window")
	}
}

func TestFlushWaitsForItsAdmissionsRatherThanLaterCompletion(t *testing.T) {
	pub := newTestBatchPublisher()
	first := pub.markAccepted()

	finished := make(chan error, 1)
	go func() { finished <- pub.Flush(context.Background()) }()
	waitForRegisteredFlushes(t, pub, 1)

	second := pub.markAccepted()
	pub.completeSequences([]uint64{second}, nil)
	select {
	case err := <-finished:
		t.Fatalf("Flush returned after later completion: %v", err)
	default:
	}

	pub.completeSequences([]uint64{first}, nil)
	if err := <-finished; err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}
}

func TestConcurrentFlushesBothReportTheSameFailedWindow(t *testing.T) {
	pub := newTestBatchPublisher()
	sequence := pub.markAccepted()
	want := errors.New("cohort aborted")
	results := make(chan error, 2)
	go func() { results <- pub.Flush(context.Background()) }()
	go func() { results <- pub.Flush(context.Background()) }()

	waitForRegisteredFlushes(t, pub, 2)

	pub.completeSequences([]uint64{sequence}, want)
	for range 2 {
		if err := <-results; !errors.Is(err, want) {
			t.Fatalf("concurrent Flush() = %v, want %v", err, want)
		}
	}

	next := pub.markAccepted()
	pub.completeSequences([]uint64{next}, nil)
	if err := pub.Flush(context.Background()); err != nil {
		t.Fatalf("healthy Flush after reporting failure = %v", err)
	}
}

func waitForRegisteredFlushes(t *testing.T, pub *batchPublisher, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		pub.stateMu.Lock()
		waiting := len(pub.activeFlushes)
		pub.stateMu.Unlock()
		if waiting == count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("registered Flush calls = %d, want %d", waiting, count)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestJoinAsyncCohortReportsThePartiallySentPrefix(t *testing.T) {
	sent := make([]nats.PubAckFuture, 59)
	stalled := errors.New("nats: too many outstanding async published messages")

	stored := joinAsyncCohort(128, sent, stalled, nil)
	if !errors.Is(stored, stalled) {
		t.Fatalf("partial send error = %v, want the send failure", stored)
	}
	if !strings.Contains(stored.Error(), "59/128") {
		t.Fatalf("partial send error %q does not report how much reached the wire", stored)
	}

	unresolved := errors.New("bus: asynchronous publish cohort PubAck timeout")
	both := joinAsyncCohort(128, sent, stalled, unresolved)
	if !errors.Is(both, stalled) || !errors.Is(both, unresolved) {
		t.Fatalf("partial send with unresolved acks = %v, want both causes", both)
	}
	if err := joinAsyncCohort(128, sent, nil, nil); err != nil {
		t.Fatalf("complete cohort = %v, want nil", err)
	}
}

func TestPublishCohortIndividuallyAwaitsTheMessagesItSent(t *testing.T) {
	js := &stallingJetStream{limit: 3}
	worker := &publishBatchWorker{js: js}

	err := worker.publishCohortIndividually(stagedBatch(5))

	if !errors.Is(err, nats.ErrTooManyStalledMsgs) {
		t.Fatalf("stalled cohort = %v, want the stall cause", err)
	}
	if js.calls != 4 {
		t.Fatalf("publish attempts = %d, want 4 (three accepted, one refused)", js.calls)
	}
	if js.awaited != 3 {
		t.Fatalf("awaited %d of the 3 messages already on the wire", js.awaited)
	}
	if !strings.Contains(err.Error(), "3/5") {
		t.Fatalf("stalled cohort error %q does not report how much reached the wire", err)
	}
}

type stallingJetStream struct {
	nats.JetStreamContext
	limit   int
	calls   int
	awaited int
}

func (s *stallingJetStream) PublishMsgAsync(msg *nats.Msg, _ ...nats.PubOpt) (nats.PubAckFuture, error) {
	s.calls++
	if s.calls > s.limit {
		return nil, nats.ErrTooManyStalledMsgs
	}
	return &settledPubAckFuture{owner: s, msg: msg}, nil
}

type settledPubAckFuture struct {
	owner *stallingJetStream
	msg   *nats.Msg
}

func (f *settledPubAckFuture) Ok() <-chan *nats.PubAck {
	f.owner.awaited++
	ok := make(chan *nats.PubAck, 1)
	ok <- &nats.PubAck{Stream: "BAGEL_DATA"}
	return ok
}

func (f *settledPubAckFuture) Err() <-chan error { return make(chan error) }

func (f *settledPubAckFuture) Msg() *nats.Msg { return f.msg }
