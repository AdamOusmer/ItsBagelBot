// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

const (
	defaultPublishBatchSize = 128
	defaultPublishBatchWait = time.Millisecond
	defaultPublishAckWait   = 2 * time.Second
	defaultPublishQueueSize = 16_384
	maxInflightCohorts      = 4
	maxInflightCohortSlots  = 64

	messageIDHeader = "Bagelbot-Message-Id"
)

// publishInflightCohorts bounds how many cohorts one stream worker keeps in
// flight. It was a hardcoded 4 because the hub admitted 50 open batches per
// stream and three pods at 4 slots each had to fit under that; the hub cap is
// now 200, so the default stays 4 but raising becomes an arithmetic question
// instead of a code change. Each extra slot parks one more staged cohort, and
// on the atomic wire a cohort's commit verdict is all-or-nothing, so the blast
// radius of one ambiguous ack grows with the slot count — the ceiling exists
// to keep a manifest typo from multiplying parked bytes, not to mark a tuned
// optimum. One slot still makes progress, so there is no floor to defend.
func publishInflightCohorts() int {
	n := env.GetInt("NATS_PUBLISH_INFLIGHT_COHORTS", maxInflightCohorts)
	return min(max(n, 1), maxInflightCohortSlots)
}

// StreamRouter is the strategy used to select a pooled connection. The default
// hashes the stream plus optional aggregate partition. Calls without a
// partition preserve stream-wide order; partitioned calls preserve order for
// that channel/tenant while allowing unrelated aggregates to publish in parallel.
type StreamRouter interface {
	Connection(stream string, poolSize int) int
}

type hashStreamRouter struct{}

func (hashStreamRouter) Connection(stream string, poolSize int) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(stream); i++ {
		hash ^= uint32(stream[i])
		hash *= 16777619
	}
	return int(hash % uint32(poolSize))
}

type publisherPool struct {
	members     []*batchPublisher
	router      StreamRouter
	fixedStream string
}

// batchPublisher is one pooled connection with one active-object batcher per
// JetStream stream assigned by StreamRouter.
type batchPublisher struct {
	nc  *nats.Conn
	js  nats.JetStreamContext
	log *zap.Logger

	// wire selects the cohort protocol (per-message PubAcks, ADR-050 atomic
	// batches or Fast-Ingest sessions). modern is the jetstream.JetStream handle
	// Orbit's batch publishers require; js stays for the per-message wire.
	wire   wireMode
	modern jsapi.JetStream

	// mu guards closed and workers together, so a single read acquisition covers
	// an entire admission: closed check, worker lookup, queue send, accept count.
	// Readers of an RWMutex do not contend each other — two RLocks on one mutex
	// never serialize — and Close was already the only writer this state had.
	mu      sync.RWMutex
	closed  bool
	workers map[string]*publishBatchWorker

	stateMu sync.Mutex
	// accepted counts messages a worker has taken, and is deliberately outside
	// stateMu because nothing ever waits on it: Flush snapshots it once as its
	// target and then waits only for completed to reach that snapshot. An
	// accept-side notification could not satisfy any waiter, so accepting costs
	// no lock, no channel close and no channel allocation.
	accepted  atomic.Uint64
	completed uint64
	// firstErr is the first cohort failure no Flush has reported yet, and
	// firstErrAt the completion position its cohort started at. Flush is a
	// per-call result, so it takes the error out on the way past instead of
	// latching it; a latch would fail every later Flush on this connection for
	// the life of the process.
	firstErr   error
	firstErrAt uint64

	// signal wakes every Flush waiting on completed, one Broadcast per cohort
	// instead of the close-and-reallocate channel this replaced. A generation
	// counter over a single long-lived channel was the other candidate and is
	// rejected on semantics: a channel send reaches exactly one receiver, so a
	// second concurrent Flush would sleep through wakeups that close-and-
	// realloc used to deliver to all of them; Broadcast preserves exactly that.
	signal *sync.Cond
}

type publishRequest struct {
	msg       *nats.Msg
	confirmed chan error
}

type publishCommand struct {
	ctx       context.Context
	stream    string
	topic     string
	msgID     string
	payload   []byte
	confirmed bool
}

type publishBatchWorker struct {
	js       nats.JetStreamContext
	requests chan publishRequest
	stop     chan struct{}
	done     chan struct{}
	owner    *batchPublisher
	slots    chan struct{}
	acks     sync.WaitGroup

	// timer times collectBatch's collection window and is reused across cohorts:
	// NewTimer per window allocates a channel and pins a runtime timer for every
	// cohort on a timed wire. collectBatch runs only on this goroutine, so the
	// field takes no lock; run stops it on the way out.
	timer *time.Timer

	// ackTimers recycles awaitAsync's PubAck-wait timers. A single hoisted field
	// like timer above would race: up to maxInflightCohorts ack goroutines call
	// awaitAsync concurrently, each Resetting the shared timer. The pool keeps
	// one per waiter with no per-cohort allocation. Get Resets before use,
	// relying on Go 1.23 timer semantics (see armWindowTimer): a tick sent
	// before Reset cannot be received after it, so no drain is needed. run's
	// acks.Wait precedes close(done), so every borrowed timer is Stopped by its
	// putAckTimer defer before teardown finishes.
	ackTimers sync.Pool

	// Cohort shape is fixed per wire at worker creation: the Fast-Ingest wire
	// amortizes a session over a much longer collection window than the
	// per-message and atomic wires can use.
	batchSize int
	batchWait time.Duration

	// overlapCommit lets an atomic cohort's commit ack be awaited off this
	// goroutine. It trades strict cross-cohort stream order for the commit RTT;
	// publishAtomicOverlapped documents exactly what that costs.
	overlapCommit bool
	// ackFirst is read once per worker: the per-cohort env lookup it replaced
	// sat on the staging path.
	ackFirst bool
	// slotHeld records that collectTimed already took this worker's inflight
	// slot for the cohort it returned; publish consumes it.
	slotHeld bool
	// newAtomic is the test seam for Orbit's batch publisher. Production leaves
	// it nil and atomicPublisher opens the real ADR-050 session.
	newAtomic func() (atomicCohortPublisher, error)
}

// publishQueueSize bounds one worker's admission queue, NATS_PUBLISH_QUEUE_SIZE
// (default defaultPublishQueueSize, clamped 64..65536).
//
// The queue is where overload hides. Every saturated production run of
// 2026-09-01 reported commit p50 of 0.4-1.7 s while the broker's own commit
// took milliseconds: with four workers of 16,384 slots a pod parks up to
// 65,536 messages before PublishOwned ever blocks, which at 100k msg/s is
// 650 ms of pure waiting that no caller can see. A lane with a latency budget
// wants that wait bounded: at 1,024 per worker the same overload surfaces as
// backpressure within ~40 ms and the caller's context decides. The default is
// unchanged so throughput-first services keep their absorption.
func publishQueueSize() int {
	return min(max(env.GetInt("NATS_PUBLISH_QUEUE_SIZE", defaultPublishQueueSize), 64), 65_536)
}

func newPublisherPool(url string, log *zap.Logger) (Publisher, error) {
	return newPublisherPoolForStream(url, "", log)
}

func newPublisherPoolForStream(url, fixedStream string, log *zap.Logger) (Publisher, error) {
	if log == nil {
		log = zap.NewNop()
	}
	poolSize := env.GetInt("NATS_PUBLISH_CONNECTIONS", 4)
	if poolSize < 1 {
		poolSize = 1
	}
	if poolSize > 32 {
		poolSize = 32
	}
	pool := &publisherPool{members: make([]*batchPublisher, 0, poolSize), router: hashStreamRouter{}, fixedStream: fixedStream}
	wire := publishWireMode()
	for i := 0; i < poolSize; i++ {
		member, err := newBatchPublisherConnection(url, i, wire, log)
		if err != nil {
			_ = pool.Close()
			return nil, err
		}
		pool.members = append(pool.members, member)
	}
	return pool, nil
}

func newBatchPublisherConnection(url string, index int, wire wireMode, log *zap.Logger) (*batchPublisher, error) {
	nc, err := nats.Connect(busPublishURL(endpoint(url)), busOptions(clientName(fmt.Sprintf("batch-publisher-%d", index)))...)
	if err != nil {
		return nil, fmt.Errorf("bus: connect batch publisher: %w", err)
	}
	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: jetstream batch publisher: %w", err)
	}
	modern, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("bus: modern jetstream batch publisher: %w", err)
	}
	publisher := &batchPublisher{
		nc: nc, js: js, modern: modern, log: log, wire: wire,
		workers: make(map[string]*publishBatchWorker),
	}
	publisher.signal = sync.NewCond(&publisher.stateMu)
	return publisher, nil
}

// nuidPool hands out NUID generators. The package-global nuid.Next() serializes
// every publishing goroutine in the process on one mutex; a borrowed generator
// produces the same identity with no shared lock. Each *nuid.NUID seeds its own
// random prefix, so ids stay unique across generators and across restarts.
var nuidPool = sync.Pool{New: func() any { return nuid.New() }}

// wireMsgPool recycles the per-publish *nats.Msg envelope. Profiling the
// saturated publish path attributed ~38% of allocation traffic to
// publishMessage's header map + value slice plus NewMsg itself; the envelope
// is fully consumed once its cohort resolves (staging serialized the bytes,
// fallback replays run before finish), so finish returns it here. Data and
// identity values are owned per message and reset by the acquirer.
var wireMsgPool = sync.Pool{New: func() any { return nats.NewMsg("") }}

// nextNUID borrows a generator for exactly one id. Returning it immediately
// keeps the pool sized to concurrent publishers rather than to every goroutine
// that has ever published, and sync.Pool's per-P cache makes the round trip
// cheaper than contending the global.
func nextNUID() string {
	generator := nuidPool.Get().(*nuid.NUID)
	id := generator.Next()
	nuidPool.Put(generator)
	return id
}

func (p *publisherPool) PublishOwned(ctx context.Context, topic string, payload []byte) error {
	// NUID avoids UUIDv7's wall-clock/random-source coordination on the hot path,
	// and a pooled generator avoids nuid.Next's process-global lock as well.
	return p.publish(publishCommand{ctx: ctx, topic: topic, msgID: nextNUID(), payload: payload})
}

func (p *publisherPool) PublishOwnedWithID(ctx context.Context, topic, msgID string, payload []byte) error {
	if msgID == "" {
		return errors.New("bus: confirmed publish requires a message ID")
	}
	return p.publish(publishCommand{ctx: ctx, topic: topic, msgID: msgID, payload: payload, confirmed: true})
}

func (p *publisherPool) publish(command publishCommand) error {
	stream, err := p.streamFor(command.topic)
	if err != nil {
		return err
	}
	command.stream = stream
	return p.connectionFor(command).publish(command)
}

func (p *publisherPool) streamFor(topic string) (string, error) {
	if p.fixedStream != "" {
		return p.fixedStream, nil
	}
	return streamForTopic(topic)
}

func (p *publisherPool) connectionFor(command publishCommand) *batchPublisher {
	routeKey := command.stream
	if partition := publishPartition(command.ctx); partition != "" {
		routeKey += "\x00" + partition
	}
	return p.members[p.router.Connection(routeKey, len(p.members))]
}

func (p *publisherPool) Flush(ctx context.Context) error {
	for _, member := range p.members {
		if err := member.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (p *publisherPool) Close() error {
	var errs []error
	for _, member := range p.members {
		if err := member.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bus: close publisher pool: %w", errs[0])
	}
	return nil
}

func (p *batchPublisher) publish(command publishCommand) error {
	request := newPublishRequest(command, publishMessage(command))
	if err := p.admit(command.ctx, command.stream, request); err != nil {
		return err
	}
	return awaitPublishConfirmation(command.ctx, request)
}

// resetWireHeader prepares a pooled envelope's header map for reuse: nil gets
// an initial map sized for the identity key plus the trace-header pair;
// otherwise every prior key except the identity slot is dropped. The identity
// slot keeps its envelope-owned one-element value slice across pool cycles —
// once the map itself was pooled, profiling still showed a fresh []string
// allocated per publish at exactly this key, so the slot survives the reset
// and publishMessage overwrites element zero. Returns the map to write into.
func resetWireHeader(h nats.Header) nats.Header {
	if h == nil {
		h = make(nats.Header, 2)
	} else {
		for k := range h {
			if k == messageIDHeader {
				continue
			}
			delete(h, k)
		}
	}
	id, ok := h[messageIDHeader]
	if !ok || cap(id) != 1 {
		// Anything not shaped like the envelope's own slot was grown by
		// somebody else's append; replace it rather than inherit the capacity,
		// so the slot's shape stays identical every cycle. The publish
		// lifecycle never grows it, so this arm runs once per envelope.
		id = make([]string, 1)
	} else {
		id = id[:1]
		id[0] = ""
	}
	h[messageIDHeader] = id
	return h
}

// attachTraceHeaders copies NewRelic's distributed-trace headers onto the
// wire. Both identity keys are canonical MIME form, so identity assignment in
// publishMessage skips Set's append path; this loop keeps Set: its keys arrive
// from NewRelic with outside casing, and canonicalization is what makes later
// Get calls find them.
func attachTraceHeaders(wire *nats.Msg, ctx context.Context) {
	if txn := newrelic.FromContext(ctx); txn != nil {
		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)
		for key := range headers {
			wire.Header.Set(key, headers.Get(key))
		}
	}
}

func publishMessage(command publishCommand) *nats.Msg {
	wire := wireMsgPool.Get().(*nats.Msg)
	wire.Subject = command.topic
	wire.Reply = ""
	wire.Data = command.payload
	wire.Header = resetWireHeader(wire.Header)
	// Preserve the fleet abstraction's message identity for subscribers, but
	// deliberately omit Nats-Msg-Id. The custom header is transport metadata,
	// not a broker dedup key. Element write, not slice assignment: the slot is
	// the envelope's own, kept alive by resetWireHeader, so this reuses it
	// instead of allocating. Recycling it is safe for the same reason reusing
	// wire.Data is — the envelope is fully consumed once its cohort resolves,
	// before finish Puts it back — and the identity string itself is owned per
	// message by command.msgID.
	wire.Header[messageIDHeader][0] = command.msgID
	attachTraceHeaders(wire, command.ctx)
	return wire
}

func newPublishRequest(command publishCommand, wire *nats.Msg) publishRequest {
	request := publishRequest{msg: wire}
	if command.confirmed {
		request.confirmed = getConfirmChan()
	}
	return request
}

// confirmChanPool recycles the buffered verdict channel of confirmed publishes,
// which otherwise costs one allocation per confirmed message.
var confirmChanPool = sync.Pool{
	New: func() any { return make(chan error, 1) },
}

// getConfirmChan drains any verdict left over by a prior waiter that abandoned
// on ctx.Done after finish had already delivered into the buffer.
func getConfirmChan() chan error {
	confirmed := confirmChanPool.Get().(chan error)
	select {
	case <-confirmed:
	default:
	}
	return confirmed
}

// putConfirmChan returns a drained verdict channel to the pool. It is called
// only from awaitPublishConfirmation after receiving the verdict: that receive
// orders after finish's single send, so no writer can touch the channel again.
// An abandoned wait never puts its channel back — its verdict may still be in
// flight — and simply drops it to the garbage collector instead.
func putConfirmChan(confirmed chan error) {
	confirmChanPool.Put(confirmed)
}

// admit is the whole per-message critical section: the closed check, the worker
// lookup, the queue send and the accept count all happen under one read lock.
// Close is the only writer, so it cannot begin while any of that is in flight,
// which is why the lock is deliberately held across a send that blocks when a
// worker's queue is full. A sender parked there is already past the closed
// check, and Close waits for it rather than pulling the queue out from under
// it. Readers of an RWMutex do not contend each other, so sharing mu between
// admissions and Close costs concurrent publishers nothing.
//
// A stream's first publish cannot stay inside that critical section: creating
// its worker needs the write lock this same arm holds for reading. That arm
// drops the read lock around startWorker instead of deadlocking on itself, then
// re-enters — re-checking closed once the read lock is back, because a full
// Close may have run entirely inside the gap.
func (p *batchPublisher) admit(ctx context.Context, stream string, request publishRequest) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return errors.New("bus: publisher is closed")
	}
	if worker := p.workers[stream]; worker != nil {
		err := p.admitLocked(ctx, worker, request)
		p.mu.RUnlock()
		return err
	}
	p.mu.RUnlock()

	created, err := p.startWorker(stream)
	if err != nil {
		return err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	// startWorker ran without the read lock held, so Close may have completed in
	// between: closed can be true even though the worker was created moments
	// ago, its stop signalled while this goroutine held no lock at all.
	if p.closed {
		return errors.New("bus: publisher is closed")
	}
	return p.admitLocked(ctx, created, request)
}

// admitLocked performs the queue send and the accept count for one admission.
// The caller holds p.mu for reading across the whole call — see admit for why
// that hold deliberately spans even a send parked on a full queue.
func (p *batchPublisher) admitLocked(ctx context.Context, worker *publishBatchWorker, request publishRequest) error {
	select {
	case worker.requests <- request:
		p.markAccepted()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func awaitPublishConfirmation(ctx context.Context, request publishRequest) error {
	if request.confirmed == nil {
		return nil
	}
	select {
	case err := <-request.confirmed:
		putConfirmChan(request.confirmed)
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startWorker creates a stream's worker under the publisher write lock. Several
// first publishers may discover a stream at once — admit's unlocked miss admits
// all of them — and the re-check here is what makes exactly one of them the
// creator. It also refuses when closed: Close walks the worker map with no lock
// at all once it publishes closed, so every insert has to be ordered before
// that walk by this critical section rather than racing it.
func (p *batchPublisher) startWorker(stream string) (*publishBatchWorker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("bus: publisher is closed")
	}
	if worker := p.workers[stream]; worker != nil {
		return worker, nil
	}
	worker := &publishBatchWorker{
		js:       p.js,
		requests: make(chan publishRequest, publishQueueSize()),
		stop:     make(chan struct{}), done: make(chan struct{}), owner: p,
		slots:    make(chan struct{}, publishInflightCohorts()),
		ackFirst: env.GetBool("NATS_ATOMIC_ACK_FIRST", true),

		batchSize:     publishBatchSize(p.wire),
		batchWait:     publishBatchWait(p.wire),
		overlapCommit: atomicPublishOverlap(),
		ackTimers:     sync.Pool{New: func() any { return time.NewTimer(defaultPublishAckWait) }},
	}
	p.workers[stream] = worker
	go worker.run()
	return worker, nil
}

func (p *batchPublisher) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	flushErr := p.Flush(ctx)
	cancel()

	// The worker map needs no guard from here on: every remaining map access in
	// admit checks closed under the same lock acquisition — the fast arm before
	// its lookup, startWorker's write arm before any insert — so nothing can
	// touch the map after the write lock above publishes closed. Everything
	// inserted earlier happened-before this walk through that same lock.
	for _, worker := range p.workers {
		close(worker.stop)
	}
	for _, worker := range p.workers {
		<-worker.done
	}
	if err := p.nc.Drain(); err != nil {
		p.nc.Close()
		if flushErr != nil {
			return fmt.Errorf("bus: flush publisher: %v; drain: %w", flushErr, err)
		}
		return err
	}
	return flushErr
}

// markAccepted records one message a worker has taken. It takes no lock and
// wakes nobody: see the accepted field for why an accept can never be what a
// Flush is waiting for. complete is still the only notifier, once per cohort.
func (p *batchPublisher) markAccepted() {
	p.accepted.Add(1)
}

func (p *batchPublisher) complete(count int, err error) {
	p.stateMu.Lock()
	if err != nil && p.firstErr == nil {
		p.firstErr = err
		// Record the failure at the cohort's first message so a Flush can tell a
		// cohort overlapping its window from one made only of messages admitted
		// after the call returned its own result.
		p.firstErrAt = p.completed
	}
	p.completed += uint64(count)
	p.signal.Broadcast()
	p.stateMu.Unlock()
	if err != nil {
		p.log.Error("asynchronous NATS publish failed", zap.Int("messages", count), zap.Error(err))
	}
}

func (p *batchPublisher) Flush(ctx context.Context) error {
	// The target is a snapshot on purpose: a message accepted after this read
	// belongs to a later Flush, so the wait cannot be extended out from under
	// the caller by traffic it never emitted.
	target := p.accepted.Load()
	p.stateMu.Lock()
	// Cond.Wait has no select arm for ctx.Done, so a watcher broadcasts the
	// cancellation into the same cond; cancel plus this reap guarantee it exits
	// before Flush returns.
	ctx, cancel := context.WithCancel(ctx)
	wake := make(chan struct{})
	go func() {
		<-ctx.Done()
		p.stateMu.Lock()
		p.signal.Broadcast()
		p.stateMu.Unlock()
		close(wake)
	}()
	var err error
	for p.completed < target {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		p.signal.Wait()
	}
	if err == nil {
		err = p.takeWindowErrLocked(target)
	}
	p.stateMu.Unlock()
	cancel()
	<-wake
	return err
}

// takeWindowErrLocked returns the pending cohort failure that overlaps this
// flush window and clears it, so the next Flush answers for its own window. A
// failure recorded once the window had already fully resolved belongs to a later
// call and is left in place for it.
func (p *batchPublisher) takeWindowErrLocked(target uint64) error {
	if p.firstErr == nil || p.firstErrAt >= target {
		return nil
	}
	err := p.firstErr
	p.firstErr = nil
	p.firstErrAt = 0
	return err
}

func (w *publishBatchWorker) run() {
	defer func() {
		if w.timer != nil {
			w.timer.Stop()
		}
		w.acks.Wait()
		close(w.done)
	}()
	for {
		batch, ok := w.nextBatch()
		if !ok {
			return
		}
		w.publish(batch)
	}
}

func (w *publishBatchWorker) nextBatch() ([]publishRequest, bool) {
	select {
	case <-w.stop:
		return nil, false
	case first := <-w.requests:
		return w.collectBatch(first)
	}
}

func (w *publishBatchWorker) collectBatch(first publishRequest) ([]publishRequest, bool) {
	// A cohort never exceeds batchSize, so one allocation covers the worst case.
	// Growing from a one-element slice instead costs a realloc and copy per
	// doubling: thirteen of them per cohort on the fast wire's 8192. Every
	// batchSize source clamps to at least one, so the capacity is never below
	// the length set here.
	batch := make([]publishRequest, 1, w.batchSize)
	batch[0] = first
	if w.batchWait <= 0 {
		return w.drainReady(batch), true
	}
	w.armWindowTimer()
	return w.collectTimed(batch)
}

// drainReady fills batch from whatever is already queued, without waiting.
func (w *publishBatchWorker) drainReady(batch []publishRequest) []publishRequest {
	for len(batch) < w.batchSize {
		select {
		case request := <-w.requests:
			batch = append(batch, request)
		default:
			return batch
		}
	}
	return batch
}

// armWindowTimer arms the worker's single reused collection timer. Reset at the
// top of each window relies on Go 1.23 timer semantics (this module declares go
// 1.26.5; the same precedent is documented at rpc_pool.go): a tick this loop
// has already received cannot recur, so the common expiry needs no drain.
// A cohort that fills before the deadline leaves the timer armed for the
// next Reset, which re-arms it in place. The one race left — a tick landing
// in the channel as the cohort completes on another case — can only cut the
// next collection window short, and cohorts already end early whenever
// batchSize lands first, so no message is lost or reordered by it.
func (w *publishBatchWorker) armWindowTimer() {
	if w.timer == nil {
		w.timer = time.NewTimer(w.batchWait)
	} else {
		w.timer.Reset(w.batchWait)
	}
}

// collectTimed fills batch until full, the window expires or the worker stops;
// stop fails the partial cohort because its messages were admitted but will
// never be staged.
//
// On a slot-gated wire the window closing does not end collection by itself:
// a cohort closed while every inflight slot is busy would only queue for one,
// and every message arriving meanwhile would start another cohort behind it.
// Each of those costs the broker a commit of its own, so at a paced offered
// rate the wire degenerates into cohorts of rate x window / connections
// messages (five at 30k msg/s over six connections and 1ms). Instead the
// cohort keeps filling until a slot frees or it is full, which is exactly the
// wait its first message would have spent parked anyway; the slot comes back
// held for publish.
func (w *publishBatchWorker) collectTimed(batch []publishRequest) ([]publishRequest, bool) {
	var slots chan<- struct{} // nil until the window closes: a nil channel never selects
	for len(batch) < w.batchSize {
		select {
		case request := <-w.requests:
			batch = append(batch, request)
		case <-w.timer.C:
			if !w.slotGated() {
				return batch, true
			}
			slots = w.slots
		case slots <- struct{}{}:
			w.slotHeld = true
			return batch, true
		case <-w.stop:
			w.fail(batch, errors.New("bus: publisher closed"))
			return nil, false
		}
	}
	return batch, true
}

// slotGated reports whether this worker's cohorts wait on an inflight slot
// before they are sent: the async wire and the overlapped atomic wire do, the
// fast wire and the serial atomic wire do not.
func (w *publishBatchWorker) slotGated() bool {
	if w.owner == nil || w.slots == nil {
		return false
	}
	switch w.owner.wire {

	case wireAtomic:
		return w.overlapCommit
	case wireFast:
		return false
	default:
		return true
	}
}

// takeSlot claims an inflight slot unless collectTimed already holds one for
// this cohort.
func (w *publishBatchWorker) takeSlot(held bool) {
	if !held {
		w.slots <- struct{}{}
	}
}

// dropSlot returns a slot collectTimed held for a cohort that turned out not
// to need one.
func (w *publishBatchWorker) dropSlot(held bool) {
	if held {
		<-w.slots
	}
}

// cohortStats counts the cohorts every worker in the process sent and the
// messages they carried; the ratio is the cohort shape the broker committed.
var cohortStats struct{ cohorts, messages atomic.Uint64 }

// CohortStats reports the cohorts published so far and the messages they
// carried, so a rig can print the average cohort the broker actually saw.
func CohortStats() (cohorts, messages uint64) {
	return cohortStats.cohorts.Load(), cohortStats.messages.Load()
}

// publish drives one cohort. Cohorts are staged from this goroutine in wire
// order; what may move off it is the wait for the broker's verdict — plus, on
// the overlapping atomic path, the commit that carries the cohort's last
// message — and only as far as each wire's ordering contract allows.
func (w *publishBatchWorker) publish(batch []publishRequest) {
	held := w.slotHeld
	w.slotHeld = false
	cohortStats.cohorts.Add(1)
	cohortStats.messages.Add(uint64(len(batch)))
	switch cohortWire(w.owner.wire, len(batch)) {
	case wireAtomic:
		w.publishAtomicCohort(batch, held)
	case wireFast:
		w.dropSlot(held)
		w.finishFast(batch, w.publishFast(batch))
	default:
		w.publishAsync(batch, held)
	}
}

// publishAsync sends the cohort message by message and lets up to
// maxInflightCohorts cohorts overlap their PubAck waits. A send that fails
// part-way still leaves everything already handed to nats.go on the wire, so
// the cohort is resolved from the same goroutine that would have awaited a
// complete one.
func (w *publishBatchWorker) publishAsync(batch []publishRequest, held bool) {
	w.takeSlot(held)

	futures, startErr := w.startAsync(batch)
	w.acks.Add(1)
	go func() {
		defer w.acks.Done()
		defer func() { <-w.slots }()
		awaitErr := w.awaitAsync(futures)
		w.finish(batch, joinAsyncCohort(len(batch), futures, startErr, awaitErr))
	}()
}

// publishCohortIndividually re-publishes a whole cohort message by message and
// waits for every PubAck. Callers must have proven the broker stored nothing of
// the cohort; otherwise this path stores a prefix a second time.
func (w *publishBatchWorker) publishCohortIndividually(batch []publishRequest) error {
	futures, startErr := w.startAsync(batch)
	awaitErr := w.awaitAsync(futures)
	return joinAsyncCohort(len(batch), futures, startErr, awaitErr)
}

// startAsync sends cohorts serially from the active object, preserving wire
// order, while awaitAsync overlaps their PubAck waits. It uses only nats.go
// APIs and bounds the client's future set. Both fallbacks re-publish a
// definitely rejected cohort through it. A refused send returns the futures
// created so far rather than abandoning them: those messages are on the wire.
func (w *publishBatchWorker) startAsync(batch []publishRequest) ([]nats.PubAckFuture, error) {
	futures := make([]nats.PubAckFuture, 0, len(batch))
	for _, req := range batch {
		future, err := w.js.PublishMsgAsync(req.msg)
		if err != nil {
			return futures, fmt.Errorf("bus: async publish %s: %w", req.msg.Subject, err)
		}
		futures = append(futures, future)
	}
	return futures, nil
}

// joinAsyncCohort reports a partially sent cohort honestly. nats.go refuses a
// publish once its future set is full and once the connection is closed, but
// everything it already accepted is on the wire and is normally stored, so those
// PubAcks are resolved before the cohort is reported instead of being abandoned
// under the send error. A caller that retries on this error stores the sent
// prefix twice, which is why the message counts are in the text.
func joinAsyncCohort(size int, futures []nats.PubAckFuture, startErr, awaitErr error) error {
	if startErr == nil {
		return awaitErr
	}
	if awaitErr != nil {
		return fmt.Errorf("bus: async cohort sent %d/%d messages and their acknowledgements did not all resolve: %w",
			len(futures), size, errors.Join(startErr, awaitErr))
	}
	return fmt.Errorf("bus: async cohort sent and stored %d/%d messages; the remainder never reached the wire: %w",
		len(futures), size, startErr)
}

// awaitAsync drains one cohort's PubAcks in publish order. A timeout here is
// ambiguous for the messages still unresolved, so the cohort fails without
// replay.
func (w *publishBatchWorker) awaitAsync(futures []nats.PubAckFuture) error {
	timer := w.ackTimer()
	defer w.putAckTimer(timer)
	for _, future := range futures {
		select {
		case <-future.Ok():
		case err := <-future.Err():
			return err
		case <-timer.C:
			return errors.New("bus: asynchronous publish cohort PubAck timeout")
		}
	}
	return nil
}

// ackTimer borrows a PubAck-wait timer from the worker's pool, Reset for this
// cohort; see ackTimers for why this is a pool rather than one field and why
// Reset needs no drain.
func (w *publishBatchWorker) ackTimer() *time.Timer {
	if timer, ok := w.ackTimers.Get().(*time.Timer); ok {
		timer.Reset(defaultPublishAckWait)
		return timer
	}
	return time.NewTimer(defaultPublishAckWait)
}

// putAckTimer stops and returns a borrowed timer. Stop after a fire is a no-op
// under Go 1.23 semantics — the tick was already paired with our receive or is
// discarded by Stop.
func (w *publishBatchWorker) putAckTimer(timer *time.Timer) {
	timer.Stop()
	w.ackTimers.Put(timer)
}

func (w *publishBatchWorker) fail(batch []publishRequest, err error) {
	w.finish(batch, err)
}

func (w *publishBatchWorker) finish(batch []publishRequest, err error) {
	w.owner.complete(len(batch), err)
	for i := range batch {
		if batch[i].confirmed != nil {
			batch[i].confirmed <- err
		}
		// Recycle only a cohort the broker acknowledged in full. On a failed
		// async cohort nats.go still owns the envelopes: it keeps them
		// registered until its own ack timeout and re-sends them on a
		// no-responders reply, so a blanked or re-leased envelope would go
		// out on the wire.
		if err != nil {
			continue
		}
		batch[i].msg.Subject = ""
		batch[i].msg.Data = nil
		wireMsgPool.Put(batch[i].msg)
	}

}
