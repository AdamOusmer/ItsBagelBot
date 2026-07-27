package bus

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"go.uber.org/zap"
)

func TestPublishWireModeDefaultsToFast(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "")
	if publishWireMode() != wireFast {
		t.Fatal("unset NATS_PUBLISH_WIRE must select the Fast-Ingest wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "nonsense")
	if publishWireMode() != wireFast {
		t.Fatal("unknown NATS_PUBLISH_WIRE must fall back to the default wire")
	}
}

func TestPublishWireModeSelectsExplicitWires(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	if publishWireMode() != wireAtomic {
		t.Fatal("NATS_PUBLISH_WIRE=atomic must select the ADR-050 batch wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "single")
	if publishWireMode() != wireSingle {
		t.Fatal("NATS_PUBLISH_WIRE=single must select the per-message PubAck wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "fast")
	if publishWireMode() != wireFast {
		t.Fatal("NATS_PUBLISH_WIRE=fast must select the Fast-Ingest wire")
	}
}

func TestCohortWireBypassesBatchingForLoneMessage(t *testing.T) {
	if got := cohortWire(wireFast, 1); got != wireSingle {
		t.Fatalf("one-message fast cohort = %v, want the plain async wire", got)
	}
	if got := cohortWire(wireAtomic, 1); got != wireSingle {
		t.Fatalf("one-message atomic cohort = %v, want the plain async wire", got)
	}
	if got := cohortWire(wireFast, 2); got != wireFast {
		t.Fatalf("fast cohort = %v, want the Fast-Ingest wire", got)
	}
	if got := cohortWire(wireAtomic, 2); got != wireAtomic {
		t.Fatalf("atomic cohort = %v, want the ADR-050 wire", got)
	}
	if got := cohortWire(wireSingle, 128); got != wireSingle {
		t.Fatalf("single-wire cohort = %v, want the plain async wire", got)
	}
}

func TestFastPublishSettingsAreBounded(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "0")
	t.Setenv("NATS_FAST_PUBLISH_FLOW", "999999")
	t.Setenv("NATS_FAST_PUBLISH_OUTSTANDING_ACKS", "-1")

	if got := publishBatchSize(wireFast); got != 1 {
		t.Fatalf("fast batch size = %d, want lower bound 1", got)
	}
	if got := fastPublishFlow(1000, 8); got != 124 {
		t.Fatalf("fast flow = %d, want useful cohort ceiling 124", got)
	}
	if got := fastPublishOutstanding(); got != 1 {
		t.Fatalf("fast outstanding = %d, want lower bound 1", got)
	}
	if got := publishBatchSize(wireSingle); got != defaultPublishBatchSize {
		t.Fatalf("single batch size = %d, want %d", got, defaultPublishBatchSize)
	}
}

func TestFastPublishDefaultsToLongSession(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "")
	if got := publishBatchSize(wireFast); got != 8192 {
		t.Fatalf("default fast session size = %d, want 8192", got)
	}
}

// The broker enforces no maximum fast-batch size, so the clamp is a chosen
// blast-radius bound rather than a protocol ceiling; it still has to bind.
func TestFastPublishSessionSizeIsClampedToTheChosenBound(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_SIZE", "200000")
	if got := publishBatchSize(wireFast); got != fastSessionMax {
		t.Fatalf("fast session size = %d, want the chosen bound %d", got, fastSessionMax)
	}
}

func TestFastPublishBatchWaitIsBounded(t *testing.T) {
	t.Setenv("NATS_FAST_PUBLISH_BATCH_WAIT", "500ms")
	if got := publishBatchWait(wireFast); got != 100*time.Millisecond {
		t.Fatalf("fast batch wait = %s, want upper bound 100ms", got)
	}
	if got := publishBatchWait(wireSingle); got != defaultPublishBatchWait {
		t.Fatalf("single batch wait = %s, want %s", got, defaultPublishBatchWait)
	}
}

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

// The fast terminal count is the highest batch sequence the broker accepted, so
// a short one proves only that the session did not cover every sequence sent.
// What landed comes from the flow acknowledgements, and the report must say both.
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

// Fast-Ingest persists on arrival, so a dead session leaves a stored prefix.
// The prefix the broker acknowledged must survive the failure verdict.
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

// Orbit never writes the initialErrCh it reads, so a rejection of the session's
// first message reaches only the error handler while AddMsg times out. The typed
// rejection is what has to be reported and what makes the replay safe.
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

// Past the first message a typed error no longer proves nothing was stored:
// everything the broker already took is in the stream and visible to consumers.
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

// Orbit's FastPublisher has no Discard: without a Close the ack-inbox
// subscription and the broker's per-stream inflight slot survive every abandoned
// cohort, so each path that leaves the session unfinished has to close it once.
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
		errs := session.asyncErr
		if errs == nil {
			errs = &firstAsyncPublishError{}
		}
		outcome := publishFastCohort(session.publisher, stagedBatch(3), errs)
		if outcome.err == nil {
			t.Fatalf("%s: expected a failed cohort", name)
		}
		if session.publisher.closeCalls != 1 {
			t.Fatalf("%s: session closed %d times, want exactly 1", name, session.publisher.closeCalls)
		}
	}
}

// A session that reached its commit is already finished on Orbit's side: the
// commit unsubscribes the ack inbox whatever the broker answered, so a short
// terminal ack is a reporting problem, not a leak, and must not send a second
// end-of-batch.
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

// A failed Fast-Ingest cohort has two verdicts, not one: the acknowledged prefix
// is durable and a confirmed caller must not retry it.
func TestFinishFastSplitsTheAcknowledgedPrefixFromTheFailedSuffix(t *testing.T) {
	worker := &publishBatchWorker{owner: newTestBatchPublisher()}
	batch := confirmedBatch(5)
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
}

func TestStripOrbitBatchFramingKeepsFleetIdentity(t *testing.T) {
	batch := framedBatch(2)

	stripOrbitBatchFraming(batch)

	for i := range batch {
		assertBatchFramingStripped(t, batch[i].msg)
		assertMessageIdentityPreserved(t, batch[i].msg)
		assertBrokerDedupAbsent(t, batch[i].msg)
	}
}

func TestCollectBatchUsesTheWireCohortShape(t *testing.T) {
	worker := &publishBatchWorker{
		requests:  make(chan publishRequest, 4),
		stop:      make(chan struct{}),
		batchSize: 2,
		batchWait: time.Second,
	}
	staged := stagedBatch(3)
	worker.requests <- staged[1]
	worker.requests <- staged[2]

	batch, ok := worker.collectBatch(staged[0])
	if !ok || len(batch) != 2 {
		t.Fatalf("collectBatch() = (%d, %v), want the configured cohort size 2", len(batch), ok)
	}
}

func TestCollectBatchStopsAtTheWireWait(t *testing.T) {
	worker := &publishBatchWorker{
		requests:  make(chan publishRequest, 1),
		stop:      make(chan struct{}),
		batchSize: defaultPublishBatchSize,
		batchWait: time.Millisecond,
	}

	batch, ok := worker.collectBatch(stagedBatch(1)[0])
	if !ok || len(batch) != 1 {
		t.Fatalf("collectBatch() = (%d, %v), want the single staged message", len(batch), ok)
	}
}

// stubFastPublisher stands in for Orbit's FastPublisher so session failure
// handling is exercised without a broker.
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

// Close answers the way Orbit does once a session is over: nothing left to
// commit. What the seam needs is that it was called at all.
func (p *stubFastPublisher) Close(context.Context) (*jetstreamext.BatchAck, error) {
	p.closeCalls++
	return nil, jetstreamext.ErrBatchClosed
}

// fixedAckSequences replays the flow-ack sequence Orbit reports per Add. The
// server acknowledges the first message with sequence 0 and then only every
// flow-th message, so the series is flat and lags the messages sent.
func fixedAckSequences(sequences ...uint64) func(call int) *jetstreamext.FastPubAck {
	return func(call int) *jetstreamext.FastPubAck {
		if call > len(sequences) {
			return &jetstreamext.FastPubAck{BatchSequence: uint64(call)}
		}
		return &jetstreamext.FastPubAck{BatchSequence: uint64(call), AckSequence: sequences[call-1]}
	}
}

func assertBatchFramingStripped(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Reply != "" {
		t.Fatalf("kept batch reply %q after strip", msg.Reply)
	}
	for _, header := range []string{
		jetstreamext.BatchIDHeader,
		jetstreamext.BatchSeqHeader,
		jetstreamext.BatchCommitHeader,
	} {
		if value := msg.Header.Get(header); value != "" {
			t.Fatalf("kept batch header %s=%q after strip", header, value)
		}
	}
}

func assertMessageIdentityPreserved(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Header.Get(messageIDHeader) == "" {
		t.Fatal("lost its fleet message id during strip")
	}
	if msg.Header.Get(legacyMessageIDHeader) == "" {
		t.Fatal("lost its rollout compatibility id during strip")
	}
}

func assertBrokerDedupAbsent(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Header.Get(nats.MsgIdHdr) != "" {
		t.Fatal("unexpectedly carries broker dedup")
	}
}

// framedBatch stages a cohort already carrying Orbit's batch framing, as it
// looks when a rejected batch reaches the fallback.
func framedBatch(n int) []publishRequest {
	batch := stagedBatch(n)
	for i := range batch {
		batch[i].msg.Header.Set(jetstreamext.BatchIDHeader, "B1")
		batch[i].msg.Header.Set(jetstreamext.BatchSeqHeader, itoa(i+1))
		batch[i].msg.Reply = "_INBOX.batch"
	}
	batch[len(batch)-1].msg.Header.Set(jetstreamext.BatchCommitHeader, "1")
	return batch
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func stagedBatch(n int) []publishRequest {
	batch := make([]publishRequest, 0, n)
	for i := 0; i < n; i++ {
		msg := nats.NewMsg("data.test.batch")
		msg.Header.Set(messageIDHeader, "id-"+itoa(i+1))
		msg.Header.Set(legacyMessageIDHeader, "id-"+itoa(i+1))
		batch = append(batch, publishRequest{msg: msg})
	}
	return batch
}

// confirmedBatch stages a cohort whose callers are all waiting on a per-message
// verdict, as PublishConfirmed leaves them.
func confirmedBatch(n int) []publishRequest {
	batch := stagedBatch(n)
	for i := range batch {
		batch[i].confirmed = make(chan error, 1)
	}
	return batch
}

// newTestBatchPublisher builds the connection-side state cohort resolution
// touches, without a broker connection.
func newTestBatchPublisher() *batchPublisher {
	return &batchPublisher{log: zap.NewNop(), changed: make(chan struct{})}
}
