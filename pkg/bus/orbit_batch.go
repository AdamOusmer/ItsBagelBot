package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"go.uber.org/zap"
)

// NATS 2.14 batch publishing (ADR-050) writes a cohort as one batch and the
// broker answers with a single commit PubAck instead of one PubAck per message.
// That removes the dominant per-event cost of the async publish path — the
// reply-subject routing and ack message for every event. Fleet publishing
// deliberately omits Nats-Msg-Id so every wire avoids the broker's per-message
// dedup index.
//
// The wire protocol itself belongs to Orbit's jetstreamext client; this file
// only owns cohort shape, configuration bounds and failure handling.
//
// The two batch wires have different durability contracts, so they have
// different failure handling.
//
// The atomic wire is all-or-nothing: a definite server API rejection proves the
// batch was not stored and permits a per-message replay, while transport
// errors, timeouts and short commit acknowledgements are ambiguous, so the
// cohort fails without replay rather than risking a double-store.
//
// The Fast-Ingest wire is at-least-once. The broker stores each message on
// arrival rather than on commit, has no isolation and no rollback, and consumers
// see messages before the commit, so an aborted session normally leaves a stored
// prefix behind. That wire therefore reports the prefix the broker acknowledged
// as delivered and fails only the unacknowledged suffix, and it replays a cohort
// only when the broker rejected the session's very first message — the one case
// that proves nothing was stored.

// wireMode selects how a publish cohort reaches the broker.
type wireMode int

const (
	// wireSingle publishes every message with its own nats.go PubAck. It is the
	// compatibility fallback for brokers without NATS 2.14 batch publishing.
	wireSingle wireMode = iota
	// wireAtomic publishes a cohort through Orbit's ADR-050 batch client: one
	// bounded cohort, one confirmed commit.
	wireAtomic
	// wireFast publishes a flow-controlled Fast-Ingest session through Orbit.
	// It is the throughput default.
	wireFast
)

// atomicBatchMax is ADR-050's per-batch message ceiling. The atomic wire
// collects cohorts of defaultPublishBatchSize, far below it; this guard only
// protects against a future batch-size bump past the protocol.
const atomicBatchMax = 1000

// fastSessionMax bounds one Fast-Ingest session. It is not a protocol limit:
// the broker enforces no maximum batch size on the fast path (jetstream.limits
// .batch.max_msgs is atomic-only) and the batch sequence is a plain uint64. The
// bound chosen here is client blast radius plus the ack-timeout budget. A failed
// session resolves as one acknowledged prefix and one unacknowledged suffix
// whose fate the caller cannot narrow further, and the worker owning it is
// blocked for up to defaultPublishAckWait per stalled acknowledgement, so a
// session stays small enough that one abort costs a bounded number of messages
// and a bounded stall.
const fastSessionMax = 65_536

func publishWireMode() wireMode {
	switch env.Get("NATS_PUBLISH_WIRE", "fast") {
	case "single":
		return wireSingle
	case "atomic":
		return wireAtomic
	default:
		return wireFast
	}
}

// cohortWire picks the wire for one collected cohort. A single-message cohort
// gains nothing from batch framing — a lone commit is an ordinary PubAck — so
// it always takes the plain async wire.
func cohortWire(wire wireMode, cohort int) wireMode {
	if cohort < 2 {
		return wireSingle
	}
	return wire
}

func publishBatchSize(wire wireMode) int {
	if wire != wireFast {
		return defaultPublishBatchSize
	}
	size := env.GetInt("NATS_FAST_PUBLISH_BATCH_SIZE", 8192)
	if size < 1 {
		return 1
	}
	return min(size, fastSessionMax)
}

func publishBatchWait(wire wireMode) time.Duration {
	if wire != wireFast {
		return defaultPublishBatchWait
	}
	wait := env.GetDuration("NATS_FAST_PUBLISH_BATCH_WAIT", 10*time.Millisecond)
	return min(max(wait, time.Millisecond), 100*time.Millisecond)
}

func fastPublishFlow(batchSize int, outstanding uint16) uint16 {
	flow := env.GetInt("NATS_FAST_PUBLISH_FLOW", 100)
	// Orbit stalls only after Flow*MaxOutstandingAcks messages. Keeping that
	// threshold below a full configured cohort exercises flow acknowledgements
	// instead of making the setting inert. Unlike the session size this really is
	// a uint16 protocol field: it is the flow token of the reply subject.
	maxUseful := max((batchSize-1)/max(int(outstanding), 1), 1)
	return uint16(min(max(flow, 1), min(maxUseful, int(^uint16(0)))))
}

func fastPublishOutstanding() uint16 {
	outstanding := env.GetInt("NATS_FAST_PUBLISH_OUTSTANDING_ACKS", 8)
	return uint16(min(max(outstanding, 1), int(^uint16(0))))
}

// publishAtomic delegates the complete ADR-050 wire contract to Orbit. The
// worker owns the batch from first Add through Commit so this stream's wire
// order is preserved. Only cohorts of one or more messages reach it.
func (w *publishBatchWorker) publishAtomic(batch []publishRequest) error {
	if len(batch) < 1 || len(batch) > atomicBatchMax {
		return fmt.Errorf("bus: atomic cohort of %d messages is outside the 1..%d server range", len(batch), atomicBatchMax)
	}
	publisher, err := jetstreamext.NewBatchPublisher(
		w.owner.modern,
		jetstreamext.BatchFlowControl{AckFirst: true, AckTimeout: defaultPublishAckWait},
	)
	if err != nil {
		return err
	}
	last := len(batch) - 1
	if err := addAtomicBatch(publisher, batch[:last]); err != nil {
		_ = publisher.Discard()
		return w.atomicFallback(batch, err)
	}
	return w.commitAtomic(publisher, batch, batch[last].msg)
}

func addAtomicBatch(publisher jetstreamext.BatchPublisher, batch []publishRequest) error {
	for i := range batch {
		if err := publisher.AddMsg(batch[i].msg); err != nil {
			return err
		}
	}
	return nil
}

func (w *publishBatchWorker) commitAtomic(publisher jetstreamext.BatchPublisher, batch []publishRequest, commit *nats.Msg) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishAckWait)
	ack, err := publisher.CommitMsg(ctx, commit)
	cancel()
	if err != nil {
		// Discard is neither necessary nor useful here: it only flips a local
		// flag, and the broker abandons an uncommitted batch on its own timeout.
		return w.atomicFallback(batch, err)
	}
	// On this wire the terminal count really is a stored-message count, so it is
	// a durability check rather than the coverage check the fast wire needs.
	if ack == nil || ack.BatchSize != uint64(len(batch)) {
		return fmt.Errorf("bus: Orbit atomic commit stored %d/%d messages", orbitBatchSize(ack), len(batch))
	}
	return nil
}

// atomicFallback re-publishes a cohort message by message, but only when the
// broker definitely rejected the batch. Every other cause is ambiguous and
// fails the cohort without replay.
func (w *publishBatchWorker) atomicFallback(batch []publishRequest, cause error) error {
	if !brokerRejectedBatch(cause) {
		return cause
	}
	w.owner.log.Warn("Orbit atomic batch rejected; re-publishing cohort individually",
		zap.Int("messages", len(batch)), zap.Error(cause))
	stripOrbitBatchFraming(batch)
	return w.publishCohortIndividually(batch)
}

// brokerRejectedBatch reports whether the broker answered with a typed API
// error. Only that answer proves nothing was stored.
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

// stripOrbitBatchFraming reverts a cohort staged for a batch so it can be
// re-published individually: leftover batch headers would make the broker treat
// each message as part of an unknown batch, and the stale reply subject must
// not leak into nats.go's own async reply management.
func stripOrbitBatchFraming(batch []publishRequest) {
	for i := range batch {
		msg := batch[i].msg
		msg.Header.Del(jetstreamext.BatchIDHeader)
		msg.Header.Del(jetstreamext.BatchSeqHeader)
		msg.Header.Del(jetstreamext.BatchCommitHeader)
		msg.Reply = ""
	}
}

// fastCohortPublisher is the seam Orbit's FastPublisher fills, so cohort
// failure handling is testable without a broker. Close belongs to the seam
// because a Fast-Ingest session has no Discard: closing is the only way to give
// back the ack inbox and the broker's inflight slot.
type fastCohortPublisher interface {
	AddMsg(*nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.FastPubAck, error)
	CommitMsg(context.Context, *nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error)
	Close(context.Context) (*jetstreamext.BatchAck, error)
}

// fastCohortOutcome is what a Fast-Ingest session actually achieved. This wire
// stores on arrival, so a failed session is not an empty one and the cohort
// cannot be resolved with a single verdict.
type fastCohortOutcome struct {
	// acked is the number of leading cohort messages the broker acknowledged as
	// persisted. Batch sequence s is cohort index s-1, so the highest flow-ack
	// sequence doubles as that count; under gap:fail the server documents a flow
	// ack as "everything up to and including this sequence was stored".
	acked int
	// streamSeq is the terminal acknowledgement's stream sequence, kept as the
	// operator-visible evidence of where a failed session actually stopped.
	streamSeq uint64
	// rejectedFirst marks a typed broker rejection of the session's first
	// message with nothing acknowledged: nothing was stored, so this cohort — and
	// only this cohort — may be replayed on the per-message wire.
	rejectedFirst bool
	err           error
}

// publishFast runs one cohort as an Orbit Fast-Ingest session. Orbit's
// FastPublisher is not safe for concurrent use, so the worker owns the session
// from first Add through Commit; that also preserves this stream's wire order.
// The block is bounded because Orbit overlaps flow acknowledgements internally.
func (w *publishBatchWorker) publishFast(batch []publishRequest) fastCohortOutcome {
	var asyncErr firstAsyncPublishError
	outstanding := fastPublishOutstanding()
	publisher, err := jetstreamext.NewFastPublisher(
		w.owner.modern,
		jetstreamext.FastPublishFlowControl{
			Flow:               fastPublishFlow(w.batchSize, outstanding),
			MaxOutstandingAcks: outstanding,
			AckTimeout:         defaultPublishAckWait,
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

// fastFallback re-publishes a cohort the broker refused at its first message.
// That rejection is the one Fast-Ingest failure that proves nothing was stored,
// so unlike every other failure on this wire it can be replayed. It covers the
// window where a stream still has allow_batched off because the service that
// owns its provisioning has not reconciled the batch features yet.
func (w *publishBatchWorker) fastFallback(batch []publishRequest, cause error) fastCohortOutcome {
	w.owner.log.Warn("Orbit Fast-Ingest session rejected at its first message; re-publishing cohort individually",
		zap.Int("messages", len(batch)), zap.Error(cause))
	stripOrbitBatchFraming(batch)
	if err := w.publishCohortIndividually(batch); err != nil {
		return fastCohortOutcome{err: err}
	}
	return fastCohortOutcome{acked: len(batch)}
}

// finishFast resolves a Fast-Ingest cohort against what the broker actually
// acknowledged. Failing the acknowledged prefix along with the rest would tell a
// confirmed caller to retry messages that are already in the stream and already
// delivered to lane consumers.
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

// storedPrefix is how many leading cohort messages may be reported as delivered:
// the whole cohort once the session committed, otherwise the prefix the broker's
// flow acknowledgements guaranteed. A failed session can never claim the whole
// cohort, because the acknowledgement that would prove it is exactly the one
// that did not arrive.
func (o fastCohortOutcome) storedPrefix(size int) int {
	if o.err == nil {
		return size
	}
	return min(max(o.acked, 0), size-1)
}

// publishFastCohort adds every message but the last, then commits with it. It
// requires a cohort of one or more messages and leaves the session closed on
// every path, so the ack inbox and the broker's inflight slot are released on
// failure as well as on success.
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

// fastSessionCause resolves what a session step actually hit. Orbit's
// fastPublisher creates and reads an initialErrCh it never writes to, so a
// broker rejection of the session's first message — 10205 batch publishing
// disabled, 10206 invalid pattern, 10211 too many inflight, sealed stream,
// insufficient resources, account limits — reaches only the error handler while
// AddMsg waits out its full ack timeout and then answers "batch message 1 ack
// timeout". The out-of-band error is therefore the authoritative one whenever it
// is set, both for reporting and for deciding whether a replay is safe.
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
	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishAckWait)
	ack, err := publisher.CommitMsg(ctx, batch[last].msg)
	cancel()
	outcome.recordTerminal(ack)
	if cause := fastSessionCause(err, asyncErr); cause != nil {
		return outcome.abort(publisher, last, cause)
	}
	return outcome.settle(ack, len(batch))
}

// observe records the persisted prefix Orbit reports on every Add. A sequence
// beyond the cohort is a protocol violation and is ignored rather than trusted.
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

// abort closes the session and records the cause. index is the cohort position
// the session failed at; only a typed broker rejection of position 0 with
// nothing acknowledged proves the cohort was not stored.
func (o fastCohortOutcome) abort(publisher fastCohortPublisher, index int, cause error) fastCohortOutcome {
	o.recordTerminal(closeFastSession(publisher))
	o.err = cause
	o.rejectedFirst = index == 0 && o.acked == 0 && brokerRejectedBatch(cause)
	return o
}

// settle applies the terminal acknowledgement. BatchSize is the highest batch
// sequence the broker accepted, not a count of stored messages: it also counts
// sequences an interest stream skipped for want of a consumer and sequences a
// duplicate suppressed. It is therefore only a check that the session saw every
// sequence this cohort sent; the count that tracks persistence is the flow
// acknowledgements' sequence, which under gap:fail with no gaps and no errors
// reaches the full cohort exactly when this coverage check passes.
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

// closeFastSession releases a Fast-Ingest session on an abort. Its EOB commit
// changes nothing about durability — this wire persists on arrival, never on
// commit — but it unsubscribes Orbit's ack inbox and gives back the broker's
// per-stream inflight fast slot instead of leaving both pinned until the
// session's inactivity timer expires. Orbit's own first-message error paths
// unsubscribe and mark the session closed before returning, so Close answers
// ErrBatchClosed there; the paths it leaks from — a publish error past the first
// message, and the stall timeout Orbit marks closed without unsubscribing — are
// exactly the ones this call can still reach or has to tolerate. The context is
// bounded so a dead session cannot hold the worker: Orbit unsubscribes on
// context cancellation too. Every Close error is expected on some abort path and
// nothing can act on it, so it is deliberately dropped.
func closeFastSession(publisher fastCohortPublisher) *jetstreamext.BatchAck {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPublishAckWait)
	defer cancel()
	ack, _ := publisher.Close(ctx)
	return ack
}

// firstAsyncPublishError captures the first error Orbit reports out of band for
// a Fast-Ingest session. Orbit calls the handler from its ack goroutine, and for
// a rejected first message it is the only place the typed API error appears.
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
