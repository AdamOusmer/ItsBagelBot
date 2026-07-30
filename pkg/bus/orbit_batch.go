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
	// bounded cohort, one confirmed commit. It is the R3 throughput wire, and it
	// is opt-in — see publishWireMode.
	//
	// The reason is server-side and structural. nats-server 2.14.3 stores an
	// atomic batch through a single ProposeMulti, so the per-stream work an
	// ingest costs — around five mutex acquisitions, a timer-heap reset and one
	// RAFT proposal — is paid ONCE PER COHORT and amortised across every message
	// in it. Nothing the client does can move that cost; only batching it can.
	wireAtomic
	// wireFast publishes a flow-controlled Fast-Ingest session through Orbit. The
	// broker persists each message as it ARRIVES, so it pays that same per-stream
	// locking, timer reset and RAFT proposal PER MESSAGE. Measured against a
	// single fast R3 stream that fits a broker service rate of about 68k msg/s,
	// whatever the publisher offers: the historical six-figure fleet numbers were
	// earned on the atomic and plain async wires, not on this one.
	//
	// It stays selectable because arrival-time persistence is a real contract —
	// consumers see a fast message before its session commits — and a workload
	// that wants it can pay the throughput for it.
	wireFast
)

// atomicBatchMax is ADR-050's per-batch message ceiling and the upper clamp on
// the configured cohort size.
const atomicBatchMax = 1000

// defaultAtomicPublishBatchSize is how many messages one atomic cohort carries
// unless NATS_ATOMIC_PUBLISH_BATCH_SIZE says otherwise.
const defaultAtomicPublishBatchSize = 256

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

// publishWireMode selects the wire for every Go publisher in the fleet.
//
// It defaults to wireSingle, and both the default and the unrecognised-value arm
// resolve there on purpose. NATS_PUBLISH_WIRE is set in no manifest under
// deploy/, so this constant IS the fleet's behaviour: sesame, outgress, users,
// projector and every data service take whatever it says the moment their image
// rolls. Defaulting to atomic would flip all of them in one deploy, with no
// per-service gate and no way to roll back short of another build.
//
// The blast radius is not symmetric either. On the single wire an ambiguous
// outcome costs one message; on the atomic wire it costs the whole cohort, up to
// defaultAtomicPublishBatchSize. And atomic cohorts draw on the same per-stream
// budget of 50 concurrently-open batches that the Elixir ingress fleet already
// sizes itself against, so a Go publisher going atomic on a stream ingress is
// also batching is a capacity change, not just a latency one.
//
// Turning it on is therefore a deliberate, per-service manifest edit that redoes
// that arithmetic — not a default. An unrecognised value picks the smallest blast
// radius rather than the largest, so a typo degrades instead of escalating.
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
		return defaultPublishBatchWait
	}
}

// atomicPublishBatchSize is the only client-side lever on the server's
// per-stream ingest cost. nats-server pays that cost — the lock acquisitions,
// the timer-heap reset and one ProposeMulti — once per cohort, so a bigger
// cohort amortises the RAFT proposal over more messages.
//
// atomicBatchMax is the protocol cap, not a chosen bound. The tradeoff that
// grows with the size is ambiguous-failure blast radius and per-cohort latency:
// the commit ack is the cohort's only verdict, so one lost or ambiguous ack
// fails every message in the cohort at once and this wire never replays an
// ambiguous cohort, and a caller waits for its whole cohort to fill and commit
// before hearing anything.
//
// The floor is two because a lone message takes the plain async wire anyway
// (cohortWire): a one-message batch is a commit with nothing to amortise.
func atomicPublishBatchSize() int {
	size := env.GetInt("NATS_ATOMIC_PUBLISH_BATCH_SIZE", defaultAtomicPublishBatchSize)
	return min(max(size, 2), atomicBatchMax)
}

// atomicPublishBatchWait bounds how long a partly filled cohort waits for more
// messages, and under load it — not the size — is what decides cohort shape: a
// cohort closes at whichever comes first, so it holds about rate x wait
// messages. A worker seeing 30k msg/s fills 30 messages in the default
// millisecond and needs about 8ms to reach 256, so raising the size without
// raising the wait changes nothing on a stream that is not already bursty.
func atomicPublishBatchWait() time.Duration {
	wait := env.GetDuration("NATS_ATOMIC_PUBLISH_BATCH_WAIT", defaultPublishBatchWait)
	return min(max(wait, 500*time.Microsecond), 20*time.Millisecond)
}

// atomicPublishOverlap reports whether a cohort's commit ack may be awaited off
// the worker goroutine. See publishAtomicOverlapped for what it costs.
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
	// Orbit stalls only after Flow*MaxOutstandingAcks messages, so keeping that
	// threshold below a full configured cohort is what exercises flow
	// acknowledgements at all. Unlike the session size this really is a uint16
	// protocol field: it is the flow token of the reply subject.
	//
	// The clamp bounds the client only. The broker dilutes its own fast-ingest
	// flow window to 500/len(sessions) per stream, so on a saturated stream the
	// server's window is far below anything this setting asks for and the knob
	// has no effect on the achieved rate. It is kept because it still decides
	// when Orbit's local stall fires, not because it tunes throughput.
	maxUseful := max((batchSize-1)/max(int(outstanding), 1), 1)
	return uint16(min(max(flow, 1), min(maxUseful, int(^uint16(0)))))
}

func fastPublishOutstanding() uint16 {
	outstanding := env.GetInt("NATS_FAST_PUBLISH_OUTSTANDING_ACKS", 8)
	return uint16(min(max(outstanding, 1), int(^uint16(0))))
}

// atomicCohortPublisher is the seam Orbit's BatchPublisher fills, so cohort
// staging, the commit overlap and failure handling are testable without a
// broker. It is deliberately the subset of the ADR-050 contract this wire uses.
type atomicCohortPublisher interface {
	AddMsg(*nats.Msg, ...jetstreamext.BatchMsgOpt) error
	CommitMsg(context.Context, *nats.Msg, ...jetstreamext.BatchMsgOpt) (*jetstreamext.BatchAck, error)
	Discard() error
}

// atomicCohort is a staged cohort on its way to a commit. Orbit's batch
// publisher is not safe for concurrent use, so exactly one goroutine may hold
// this at a time: the worker builds it and never touches the session again, and
// the goroutine handoff gives the commit a happens-before on every Add.
type atomicCohort struct {
	publisher atomicCohortPublisher
	batch     []publishRequest
	stageErr  error
}

// publishAtomicCohort routes one cohort to the overlapping or the strictly
// ordered atomic path.
func (w *publishBatchWorker) publishAtomicCohort(batch []publishRequest) {
	if w.overlapCommit {
		w.publishAtomicOverlapped(batch)
		return
	}
	w.finish(batch, w.publishAtomic(batch))
}

// publishAtomicOverlapped stages a cohort on the worker and awaits its commit on
// its own goroutine, so the next cohort's messages are already on the wire while
// this one's commit ack is still outstanding. That ack is a RAFT quorum round
// trip on an R3 stream and used to be dead air on the worker, with only
// NATS_PUBLISH_CONNECTIONS workers per stream to hide it behind: one commit RTT
// of every cohort's wall clock spent offering the stream nothing.
//
// Orbit has no end-of-batch commit. BatchPublisher offers only
// Commit(ctx, subject, data) and CommitMsg(ctx, msg), and both PUBLISH the
// cohort's final message carrying Nats-Batch-Commit (jetstreamext/publishbatch
// .go:42-49 and :268-341), so the commit cannot be separated from that last
// publish and the overlap goroutine has to send it.
//
// What that relaxes is cross-cohort order, and by a whole cohort rather than by
// the one message: the broker stages an atomic batch and appends it with a
// single ProposeMulti AT COMMIT, so stream sequences follow commit arrival, not
// add arrival. A cohort's own messages keep their order under
// Nats-Batch-Sequence and each commit is dispatched from a goroutine started
// after the previous one, so the order is right in the ordinary case, but
// nothing forces the scheduler to keep it. NATS_ATOMIC_PUBLISH_OVERLAP=false
// restores strict cohort order by keeping the commit on the worker.
//
// The slot is taken before staging, so a worker holds at most
// maxInflightCohorts open batches. That is also what keeps the fleet under the
// broker's own cap on concurrently staged batches per stream
// (jetstream.limits.batch.max_inflight_per_stream, default 50):
// NATS_PUBLISH_CONNECTIONS workers x maxInflightCohorts slots is 16 per pod, so
// up to three pods publishing to one stream still fit.
func (w *publishBatchWorker) publishAtomicOverlapped(batch []publishRequest) {
	if err := atomicCohortFits(batch); err != nil {
		w.finish(batch, err)
		return
	}
	w.slots <- struct{}{}
	cohort := w.stageAtomic(batch)
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		w.finish(batch, w.resolveAtomic(cohort))
	}()
}

// publishAtomic runs one cohort to completion on the calling goroutine. It is
// the strict-order path and the one the fallbacks' own await stays inside.
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

// stageAtomic opens a session and publishes every message but the last, in wire
// order, from the calling goroutine. The last message is the commit's payload
// because Orbit's commit is a publish.
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

// resolveAtomic settles a staged cohort: the commit when the session is intact,
// the at-most-once fallback rules when it is not. Both the commit ack wait and
// any replay it authorises happen here, so whichever goroutine resolves a cohort
// owns every step of it and the worker is never blocked by a failure path.
func (w *publishBatchWorker) resolveAtomic(cohort atomicCohort) error {
	if cohort.stageErr != nil {
		return w.atomicFallback(cohort.batch, cohort.stageErr)
	}
	last := len(cohort.batch) - 1
	return w.commitAtomic(cohort.publisher, cohort.batch, cohort.batch[last].msg)
}

// atomicPublisher opens one ADR-050 session. AckFirst makes Orbit request-reply
// the cohort's first message, so a stream that refuses batches answers with a
// typed API error while nothing has been staged — the one failure this wire may
// safely replay. It also costs a round trip per cohort on the staging goroutine,
// which is the inline wait the commit overlap does not remove.
func (w *publishBatchWorker) atomicPublisher() (atomicCohortPublisher, error) {
	if w.newAtomic != nil {
		return w.newAtomic()
	}
	publisher, err := jetstreamext.NewBatchPublisher(
		w.owner.modern,
		jetstreamext.BatchFlowControl{AckFirst: true, AckTimeout: defaultPublishAckWait},
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
