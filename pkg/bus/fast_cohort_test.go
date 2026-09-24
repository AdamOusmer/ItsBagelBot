// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
)

func TestFastCohortCommitsWholeSession(t *testing.T) {
	publisher := &stubFastPublisher{ack: &jetstreamext.BatchAck{BatchSize: 3, Sequence: 91}}

	outcome := publishFastCohort(publisher, stagedBatch(3), &firstAsyncPublishError{})

	if outcome.err != nil {
		t.Fatalf("complete fast cohort failed: %v", outcome.err)
	}
	if outcome.acked != 3 {
		t.Fatalf("committed session acknowledged %d/3 messages", outcome.acked)
	}
	if publisher.addCalls != 2 || publisher.commitCalls != 1 {
		t.Fatalf("fast session sent %d adds and %d commits, want 2 and 1", publisher.addCalls, publisher.commitCalls)
	}
	if publisher.closeCalls != 0 {
		t.Fatalf("a committed session was closed %d times", publisher.closeCalls)
	}
}

func TestFastCohortDoesNotCommitAfterAddFailure(t *testing.T) {
	want := errors.New("add failed")
	publisher := &stubFastPublisher{addErr: want}

	outcome := publishFastCohort(publisher, stagedBatch(2), &firstAsyncPublishError{})

	if !errors.Is(outcome.err, want) {
		t.Fatalf("publishFastCohort() error = %v, want %v", outcome.err, want)
	}
	if publisher.commitCalls != 0 {
		t.Fatalf("commit called %d times after Add failure", publisher.commitCalls)
	}
}

func TestFastCohortStopsAtFirstAsyncError(t *testing.T) {
	asyncErr := &firstAsyncPublishError{}
	want := errors.New("gap detected")
	publisher := &stubFastPublisher{ack: &jetstreamext.BatchAck{BatchSize: 3}, onAdd: func() { asyncErr.set(want) }}

	outcome := publishFastCohort(publisher, stagedBatch(3), asyncErr)

	if !errors.Is(outcome.err, want) {
		t.Fatalf("publishFastCohort() error = %v, want the reported session error", outcome.err)
	}
	if publisher.commitCalls != 0 {
		t.Fatalf("commit called %d times after an out-of-band session error", publisher.commitCalls)
	}
}

func TestFastCohortFailsOnAsyncErrorReportedAtCommit(t *testing.T) {
	asyncErr := &firstAsyncPublishError{}
	want := errors.New("sequence error")
	publisher := &stubFastPublisher{ack: &jetstreamext.BatchAck{BatchSize: 2}, onCommit: func() { asyncErr.set(want) }}

	if outcome := publishFastCohort(publisher, stagedBatch(2), asyncErr); !errors.Is(outcome.err, want) {
		t.Fatalf("publishFastCohort() error = %v, want the reported session error", outcome.err)
	}
}

func TestFastCohortShortCommitAckReportsCoverageAndStoredPrefix(t *testing.T) {
	publisher := &stubFastPublisher{
		ack:    &jetstreamext.BatchAck{BatchSize: 2, Sequence: 404},
		addAck: fixedAckSequences(0, 1),
	}

	outcome := publishFastCohort(publisher, stagedBatch(3), &firstAsyncPublishError{})

	if outcome.err == nil {
		t.Fatal("a commit ack covering fewer sequences must fail the cohort")
	}
	if outcome.acked != 1 {
		t.Fatalf("stored prefix = %d, want the acknowledged flow sequence 1", outcome.acked)
	}
	for _, want := range []string{"2/3 batch sequences", "1 messages acknowledged as stored", "stream sequence 404"} {
		if !strings.Contains(outcome.err.Error(), want) {
			t.Fatalf("short-ack error %q does not report %q", outcome.err, want)
		}
	}
}

func TestFastCohortFailsOnMissingCommitAck(t *testing.T) {
	publisher := &stubFastPublisher{}
	if outcome := publishFastCohort(publisher, stagedBatch(2), &firstAsyncPublishError{}); outcome.err == nil {
		t.Fatal("a missing commit ack must fail the cohort")
	}
}

func TestFastCohortKeepsTheAcknowledgedPrefix(t *testing.T) {
	publisher := &stubFastPublisher{
		commitErr: errors.New("ack timeout"),
		addAck:    fixedAckSequences(0, 0, 0, 3, 3, 3, 6),
	}

	outcome := publishFastCohort(publisher, stagedBatch(8), &firstAsyncPublishError{})

	if outcome.err == nil {
		t.Fatal("a failed commit must fail the unacknowledged suffix")
	}
	if outcome.acked != 6 {
		t.Fatalf("stored prefix = %d, want the highest acknowledged sequence 6", outcome.acked)
	}
	if outcome.rejectedFirst {
		t.Fatal("a session that stored a prefix must never be replayed")
	}
}

func TestFastCohortPrefersTheTypedRejectionOverTheAckTimeout(t *testing.T) {
	rejected := &jsapi.APIError{Code: 400, ErrorCode: 10205, Description: "batch publish is disabled"}
	asyncErr := &firstAsyncPublishError{}
	asyncErr.set(rejected)
	publisher := &stubFastPublisher{addErr: errors.New("batch message 1 ack timeout")}

	outcome := publishFastCohort(publisher, stagedBatch(4), asyncErr)

	if !errors.Is(outcome.err, error(rejected)) {
		t.Fatalf("publishFastCohort() error = %v, want the typed broker rejection", outcome.err)
	}
	if !outcome.rejectedFirst {
		t.Fatal("a definite rejection of message 1 must permit the per-message wire")
	}
}

func TestFastCohortDoesNotReplayPastTheFirstMessage(t *testing.T) {
	rejected := &jsapi.APIError{Code: 400, ErrorCode: 10208, Description: "batch publish ID unknown"}
	asyncErr := &firstAsyncPublishError{}
	publisher := &stubFastPublisher{addAck: fixedAckSequences(0, 1)}
	publisher.onAdd = func() {
		if publisher.addCalls == 2 {
			asyncErr.set(rejected)
		}
	}

	outcome := publishFastCohort(publisher, stagedBatch(4), asyncErr)

	if !errors.Is(outcome.err, error(rejected)) {
		t.Fatalf("publishFastCohort() error = %v, want the reported rejection", outcome.err)
	}
	if outcome.rejectedFirst {
		t.Fatal("a rejection after the first message must not permit a replay")
	}
}

func TestFastCohortClosesTheSessionOnEveryAbortPath(t *testing.T) {
	asyncErr := &firstAsyncPublishError{}
	reported := &stubFastPublisher{ack: &jetstreamext.BatchAck{BatchSize: 3}}
	reported.onAdd = func() { asyncErr.set(errors.New("gap detected")) }

	for name, session := range map[string]struct {
		publisher *stubFastPublisher
		asyncErr  *firstAsyncPublishError
	}{
		"add error":         {publisher: &stubFastPublisher{addErr: errors.New("publish failed")}},
		"commit error":      {publisher: &stubFastPublisher{commitErr: errors.New("ack timeout")}},
		"out-of-band error": {publisher: reported, asyncErr: asyncErr},
	} {
		requireSessionClosedOnce(t, name, session.publisher, session.asyncErr)
	}
}

func requireSessionClosedOnce(t *testing.T, name string, publisher *stubFastPublisher, asyncErr *firstAsyncPublishError) {
	t.Helper()
	if asyncErr == nil {
		asyncErr = &firstAsyncPublishError{}
	}
	outcome := publishFastCohort(publisher, stagedBatch(3), asyncErr)
	if outcome.err == nil {
		t.Fatalf("%s: expected a failed cohort", name)
	}
	if publisher.closeCalls != 1 {
		t.Fatalf("%s: session closed %d times, want exactly 1", name, publisher.closeCalls)
	}
}

func TestFastCohortDoesNotCloseACommittedSession(t *testing.T) {
	publisher := &stubFastPublisher{ack: &jetstreamext.BatchAck{BatchSize: 2}}

	outcome := publishFastCohort(publisher, stagedBatch(3), &firstAsyncPublishError{})

	if outcome.err == nil {
		t.Fatal("a commit ack covering fewer sequences must fail the cohort")
	}
	if publisher.closeCalls != 0 {
		t.Fatalf("committed session closed %d times", publisher.closeCalls)
	}
}

func TestFastCohortStoredPrefixNeverClaimsAFailedCohort(t *testing.T) {
	committed := fastCohortOutcome{acked: 4}
	if got := committed.storedPrefix(4); got != 4 {
		t.Fatalf("committed prefix = %d, want the whole cohort", got)
	}
	failed := fastCohortOutcome{acked: 4, err: errors.New("ack timeout")}
	if got := failed.storedPrefix(4); got != 3 {
		t.Fatalf("failed prefix = %d, want at most one short of the cohort", got)
	}
	unknown := fastCohortOutcome{err: errors.New("ack timeout")}
	if got := unknown.storedPrefix(4); got != 0 {
		t.Fatalf("unacknowledged prefix = %d, want none", got)
	}
}

func TestFinishFastSplitsTheAcknowledgedPrefixFromTheFailedSuffix(t *testing.T) {
	worker := &publishBatchWorker{owner: newTestBatchPublisher()}
	batch := confirmedBatch(5)
	admitTestBatch(worker.owner, batch)
	want := errors.New("session aborted")

	worker.finishFast(batch, fastCohortOutcome{acked: 2, err: want})

	for i, request := range batch {
		got := <-request.confirmed
		if i < 2 && got != nil {
			t.Fatalf("message %d of the acknowledged prefix failed: %v", i, got)
		}
		if i >= 2 && !errors.Is(got, want) {
			t.Fatalf("message %d of the unacknowledged suffix = %v, want %v", i, got, want)
		}
	}
	if completed := worker.owner.completed; completed != uint64(len(batch)) {
		t.Fatalf("resolved %d/%d messages", completed, len(batch))
	}
}

func TestFastFallbackReplaysOnlyADefiniteFirstMessageRejection(t *testing.T) {
	rejected := &jsapi.APIError{Code: 400, ErrorCode: 10205, Description: "batch publish is disabled"}
	if !brokerRejectedBatch(rejected) {
		t.Fatal("a typed rejection of message 1 must permit the per-message wire")
	}
	outcome := fastCohortOutcome{}.abort(&stubFastPublisher{}, 0, rejected)
	if !outcome.rejectedFirst {
		t.Fatal("abort at position 0 with nothing acknowledged must permit a replay")
	}
	stored := fastCohortOutcome{acked: 1}.abort(&stubFastPublisher{}, 0, rejected)
	if stored.rejectedFirst {
		t.Fatal("an acknowledged prefix must block the replay even at position 0")
	}
}

type stubFastPublisher struct {
	addErr      error
	commitErr   error
	ack         *jetstreamext.BatchAck
	addAck      func(call int) *jetstreamext.FastPubAck
	addCalls    int
	commitCalls int
	closeCalls  int
	onAdd       func()
	onCommit    func()
}

func (p *stubFastPublisher) AddMsg(*nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.FastPubAck, error) {
	p.addCalls++
	if p.onAdd != nil {
		p.onAdd()
	}
	if p.addAck != nil {
		return p.addAck(p.addCalls), p.addErr
	}
	return &jetstreamext.FastPubAck{}, p.addErr
}

func (p *stubFastPublisher) CommitMsg(context.Context, *nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error) {
	p.commitCalls++
	if p.onCommit != nil {
		p.onCommit()
	}
	return p.ack, p.commitErr
}

func (p *stubFastPublisher) Close(context.Context) (*jetstreamext.BatchAck, error) {
	p.closeCalls++
	return nil, jetstreamext.ErrBatchClosed
}

func fixedAckSequences(sequences ...uint64) func(call int) *jetstreamext.FastPubAck {
	return func(call int) *jetstreamext.FastPubAck {
		if call > len(sequences) {
			return &jetstreamext.FastPubAck{BatchSequence: uint64(call)}
		}
		return &jetstreamext.FastPubAck{BatchSequence: uint64(call), AckSequence: sequences[call-1]}
	}
}
