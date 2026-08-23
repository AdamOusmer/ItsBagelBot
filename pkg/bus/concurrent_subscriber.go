// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

// concurrentDurableSubscriber is the fleet-owned JetStream delivery adapter.
// nats.go invokes one subscription callback serially, so the callback must not
// wait for the handler's acknowledgement: doing so hard-caps every pod at one in-flight
// event. This adapter hands the message through a bounded queue and a pump to
// the weighted routine pool, returns immediately, and reconciles Ack/Nack
// concurrently after the handler finishes.
type concurrentDurableSubscriber struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	stream   string
	consumer string
	group    string
	delay    redeliveryDelay
	// handlerDeadline is the ceiling on one handler's total run time, NOT the
	// consumer's AckWait. Those are different clocks and used to share a name.
	// awaitResult reports InProgress every `progress`, which resets the server's
	// AckWait for as long as the handler lives, so the server does not redeliver
	// underneath a slow-but-healthy handler. This is what stops that from being
	// unbounded: past it the subscriber stops reporting progress and lets AckWait
	// take the message back.
	handlerDeadline time.Duration
	progress        time.Duration
	// ackSync confirms each acknowledgement with the server instead of firing it
	// asynchronously. It is set for work-queue retention only; see ack.
	ackSync bool
	log     *zap.Logger

	// dropped counts deliveries shed when the explicit queue is full; see the
	// overflow branch in deliveryCallback for why shedding beats blocking.
	dropped atomic.Int64
	// wheel drives the shared keep-alive passes; see keepAliveWheel.
	wheel *keepAliveWheel

	mu      sync.Mutex
	closed  bool
	subs    map[*nats.Subscription]ownedSubscription
	closeCh chan struct{}

	registrations sync.WaitGroup
	acks          sync.WaitGroup
}

// callbackGate makes callback admission and shutdown one atomic decision.
// nats.go may invoke an async callback just after Unsubscribe returns, so a
// WaitGroup alone cannot safely guard channel closure: Add could race Wait.
type callbackGate struct {
	mu       sync.Mutex
	stopping bool
	stopCh   chan struct{}
	active   sync.WaitGroup
}

type concurrentSubscriberConfig struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	stream   string
	consumer string
	group    string
	delay    redeliveryDelay
	log      *zap.Logger
}

const (
	terminateDelivery      = time.Duration(-1)
	subscriberDrainTimeout = 30 * time.Second
	// laneAckWait is the server-side redelivery clock every lane consumer is
	// created with (see laneConsumerConfig). It is named here because two things
	// in this file are sized against it and a literal in three places drifts.
	laneAckWait = 4 * time.Second
	// ackSyncTimeout bounds the confirmed acknowledgement on work-queue lanes. It
	// stays under laneAckWait so a failed confirmation still has room to
	// NAK before the server redelivers on its own clock.
	ackSyncTimeout = laneAckWait / 2

	// durableQueueBytesBudget is how much payload the explicit-delivery queue
	// may hold at once. It exists so the worst case is a stated number rather
	// than something emergent: the queue decouples nats.go's serial delivery
	// goroutine from a momentarily slow reader, and this constant is the ceiling
	// on how much that decoupling can buffer before it starts shedding.
	durableQueueBytesBudget = 8 << 20

	// durableWireBytesFloor is the smallest lane delivery the queue depth is
	// sized against. Live ingress runs measured ~865 B per event on the wire;
	// rounding the average up to a flat 1 KiB deliberately rounds the derived
	// depth DOWN in count while keeping the payload bound honest — the failure
	// this budget guards against is unbounded heap under a stalled reader, never
	// a shallow queue, and the overflow counter below makes any real shallowness
	// loud instead of silent.
	durableWireBytesFloor = 1024

	// durableQueueDepth is how many deliveries the callback may hold for the
	// pump, derived from the byte budget exactly the way flowQueueDepth is
	// derived from the flow-control window (8 MiB / 1 KiB = 8192).
	//
	// Unlike the flow lane, this queue is deliberately sized UNDER the consumer's
	// MaxAckPending (default 20000, raisable via NATS_LANE_MAX_ACK_PENDING) rather
	// than above it. The flow lane had to clear its server's ramped byte window
	// because a drop there loses a receipt permanently; here the brake really is
	// the message count, and a shed delivery is one delayed event — the server
	// redelivers it after laneAckWait (4s). Blocking, the alternative this queue
	// replaced, was measured as strictly worse: parking nats.go's serial callback
	// on `output <-` stalled every server push until MaxAckPending seats
	// stranded, capping the lane near 2-3k msg/s. An unbounded queue was rejected
	// for the opposite reason: it converts downstream stall into unbounded heap.
	// Sitting under the brake means saturation sheds early and paces itself via
	// AckWait instead of accumulating resident payload, and the throttled
	// overflow counter (every 1000th drop logged, matching the flow overrun
	// counter) is the signal that the derivation needs re-measuring.
	durableQueueDepth = durableQueueBytesBudget / durableWireBytesFloor
)

// workQueueRetention reports whether a catalog stream deletes messages on
// acknowledgement. The two acknowledgement contracts differ in what a lost ACK
// costs, so the subscriber picks its ack mode from the retention policy rather
// than from a caller-supplied flag that can drift from the catalog.
func workQueueRetention(stream string) bool {
	specs := make([]StreamSpec, 0, len(DataStreams)+2)
	specs = append(specs, DataStreams...)
	specs = append(specs, OutgressStream, OutgressSystemStream)

	for _, spec := range specs {
		if spec.Name == stream {
			return spec.Retention == nats.WorkQueuePolicy
		}
	}
	return false
}

// redeliveryDelay keeps retry pacing behind the native subscriber abstraction.
// retry is JetStream's one-based NumDelivered counter.
type redeliveryDelay interface {
	WaitTime(retry uint64) time.Duration
}

type maxRetryDelay struct {
	delay time.Duration
	max   uint64
}

func newMaxRetryDelay(delay time.Duration, max uint64) maxRetryDelay {
	return maxRetryDelay{delay: delay, max: max}
}

func (d maxRetryDelay) WaitTime(retry uint64) time.Duration {
	if retry >= d.max {
		return terminateDelivery
	}
	return d.delay
}

func newConcurrentDurableSubscriber(cfg concurrentSubscriberConfig) *concurrentDurableSubscriber {
	if cfg.log == nil {
		cfg.log = zap.NewNop()
	}
	s := &concurrentDurableSubscriber{
		nc: cfg.nc, js: cfg.js, stream: cfg.stream, consumer: cfg.consumer, group: cfg.group,
		delay: cfg.delay, handlerDeadline: 30 * time.Second, progress: time.Second, log: cfg.log,
		ackSync: workQueueRetention(cfg.stream),
		subs:    make(map[*nats.Subscription]ownedSubscription), closeCh: make(chan struct{}),
	}
	// Keep the WaitGroup positive until Close has unsubscribed every callback;
	// this prevents an Add racing a Wait while a final delivery is arriving.
	s.acks.Add(1)
	// One wheel per subscriber, not one timer per message; see keepAliveWheel.
	// It lives on closeCh, so both Close paths retire it and it stays running
	// through the ack drain exactly as the per-message timers used to.
	s.wheel = newKeepAliveWheel(wheelStepCount(s.handlerDeadline, s.progress), s.progress, s.closeCh)
	return s
}

// wheelStepCount is how many progress intervals a watch may survive: the
// per-message timers reported InProgress at fire k and surrendered once
// k*progress reached handlerDeadline, so the step count is a ceiling division.
// A degenerate interval collapses to a single step rather than panicking.
func wheelStepCount(deadline, progress time.Duration) int {
	if progress <= 0 || deadline <= 0 {
		return 1
	}
	steps := (deadline + progress - 1) / progress
	if steps < 1 {
		return 1
	}
	return int(steps)
}

func (s *concurrentDurableSubscriber) Subscribe(ctx context.Context, subject string) (<-chan *Message, error) {
	output := make(chan *Message)
	callbacks := newCallbackGate()
	pump := newSubscriptionPump()

	if !s.beginRegistration() {
		return nil, errors.New("bus: subscriber is closed")
	}
	defer s.registrations.Done()

	callback := s.deliveryCallback(ctx, subject, callbacks, pump.queue)
	sub, err := s.subscribe(subject, callback)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stopSubscription(sub, callbacks, pump)
		return nil, errors.New("bus: subscriber closed during subscribe")
	}
	s.subs[sub] = ownedSubscription{sub: sub, callbacks: callbacks, pump: pump}
	s.mu.Unlock()

	go s.watchBind(ctx, sub, callbacks, pump)
	go s.runPump(ctx, output, callbacks, pump)

	return output, nil
}

// watchBind retires the subscription when its binding context ends or the
// subscriber closes, whichever comes first.
func (s *concurrentDurableSubscriber) watchBind(ctx context.Context, sub *nats.Subscription, callbacks *callbackGate, pump *subscriptionPump) {
	select {
	case <-ctx.Done():
	case <-s.closeCh:
	}
	stopSubscription(sub, callbacks, pump)
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

// runPump drains the explicit delivery queue toward the lane channel until the
// pump halts or a delivery fails, then abandons whatever remains queued.
func (s *concurrentDurableSubscriber) runPump(ctx context.Context, output chan<- *Message, callbacks *callbackGate, pump *subscriptionPump) {
	defer close(output)
	for live := true; live; {
		select {
		case d := <-pump.queue:
			live = s.deliver(ctx, output, callbacks, d)
		case <-pump.stop:
			live = false
		}
	}
	s.abandon(pump.queue)
}

func (s *concurrentDurableSubscriber) beginRegistration() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.registrations.Add(1)
	return true
}

func (s *concurrentDurableSubscriber) subscribe(subject string, callback nats.MsgHandler) (*nats.Subscription, error) {
	if s.consumer != "" {
		return s.js.QueueSubscribe(subject, s.group, callback,
			nats.Bind(s.stream, s.consumer), nats.ManualAck())
	}
	// Broadcast subscriptions are ephemeral and start at messages published
	// after the binding. Each service instance owns a distinct consumer, so a
	// cache invalidation fans out to every replica.
	return s.js.Subscribe(subject, callback,
		nats.BindStream(s.stream), nats.DeliverNew(), nats.AckExplicit(), nats.ManualAck())
}

// pendingDelivery is one decoded delivery waiting in the explicit queue for
// the pump to hand it toward the lane channel.
type pendingDelivery struct {
	msg   *Message
	watch *resultWatch
}

// subscriptionPump owns one subscription's explicit queue and the signal that
// retires it. halt is idempotent because both shutdown paths — context
// cancellation via the subscription's own watcher and Close via stopCallbacks
// — can reach it, potentially concurrently, and each must be able to fire it.
type subscriptionPump struct {
	queue chan pendingDelivery
	stop  chan struct{}
	once  sync.Once
}

func newSubscriptionPump() *subscriptionPump {
	return &subscriptionPump{
		queue: make(chan pendingDelivery, durableQueueDepth),
		stop:  make(chan struct{}),
	}
}

// halt releases the pump. It must run strictly after callbacks.stopAndWait:
// only then is every callback gone, so the pump's final non-blocking drain can
// unwind the remaining queue without racing an enqueuer.
func (p *subscriptionPump) halt() { p.once.Do(func() { close(p.stop) }) }

func (s *concurrentDurableSubscriber) deliveryCallback(
	ctx context.Context,
	subject string,
	callbacks *callbackGate,
	queue chan<- pendingDelivery,
) nats.MsgHandler {
	return func(natsMsg *nats.Msg) {
		if !callbacks.enter() {
			return
		}
		defer callbacks.leave()

		msg, err := messageFromNATS(natsMsg)
		if err != nil {
			s.terminateMalformed(natsMsg, subject, err)
			return
		}
		msg.SetContext(ctx)
		// The result watch MUST be installed before the handoff. A fast
		// worker resolves the instant the send completes, and a handler
		// installed after that winning transition is never called — the ack
		// is silently dropped. This broker never fires AckWait redelivery
		// while interest stays bound, so every dropped slot strands one
		// MaxAckPending seat until max_age; that race is what capped the
		// lane near 2-3k msg/s. This ordering survived a fixed critical bug
		// and must not move relative to the visibility point: installing
		// here, ahead of the enqueue, keeps the watch armed before the pump
		// can ever make the message visible. The non-delivery arms unwind
		// the watch themselves and resultWatch.finished makes double release
		// a no-op.
		//
		// The handoff itself is the bounded queue, and the enqueue never
		// blocks: a saturated downstream sheds one delivery instead of
		// parking nats.go's serial callback and stalling every server push.
		w := s.newResultWatch(natsMsg, msg)
		select {
		case queue <- pendingDelivery{msg: msg, watch: w}:
		default:
			// Shedding follows the flow-lane precedent: dropping costs one
			// event, blocking costs the window. On this adapter the shed
			// delivery comes back on AckWait (<= laneAckWait), so the cost is
			// delay rather than loss. Throttled like the flow overrun counter
			// — sustained overflow is exactly the case where a line per drop
			// emits at lane rate; the counter still moves on every drop, so
			// the magnitude is never lost, only the repetition.
			w.unwind()
			if dropped := s.dropped.Add(1); dropped == 1 || dropped%1_000 == 0 {
				s.log.Warn("durable delivery queue overflowed; leaving the message to AckWait redelivery",
					zap.String("subject", subject),
					zap.Int64("dropped_total", dropped))
			}
		}
	}
}

// newResultWatch arms one message's keep-alive slot on the shared wheel and
// counts it against the in-flight ack budget. The budget seat is taken before
// the watch becomes observable: registering into the wheel is the earliest
// point another goroutine — the wheel ticker — may release the seat, and the
// Add above it in program order is what keeps the counter from going negative.
func (s *concurrentDurableSubscriber) newResultWatch(natsMsg *nats.Msg, msg *Message) *resultWatch {
	w := &resultWatch{s: s, natsMsg: natsMsg}
	s.acks.Add(1)
	msg.setResolveHandler(w.resolve)
	s.wheel.register(w)
	return w
}

// deliver hands one queued message to the lane channel with its verdict
// already wired up. It reports whether the handoff won; a lost handoff
// releases the watch here so shutdown never waits on an acknowledgement
// nobody will make.
func (s *concurrentDurableSubscriber) deliver(
	ctx context.Context,
	output chan<- *Message,
	callbacks *callbackGate,
	d pendingDelivery,
) bool {
	select {
	case output <- d.msg:
		return true
	case <-ctx.Done():
	case <-s.closeCh:
	case <-callbacks.stopped():
	}
	d.watch.unwind()
	return false
}

// abandon unwinds every delivery the queue still holds. It runs strictly after
// callbacks.stopAndWait (via pumpDone), which guarantees no enqueuer remains,
// so a plain non-blocking drain cannot miss an entry.
func (s *concurrentDurableSubscriber) abandon(queue <-chan pendingDelivery) {
	for {
		select {
		case d := <-queue:
			d.watch.unwind()
		default:
			return
		}
	}
}

func newCallbackGate() *callbackGate {
	return &callbackGate{stopCh: make(chan struct{})}
}

func (g *callbackGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping {
		return false
	}
	g.active.Add(1)
	return true
}

func (g *callbackGate) leave() { g.active.Done() }

func (g *callbackGate) stopped() <-chan struct{} { return g.stopCh }

func (g *callbackGate) stopAndWait() {
	g.mu.Lock()
	if !g.stopping {
		g.stopping = true
		close(g.stopCh)
	}
	g.mu.Unlock()
	g.active.Wait()
}

func stopSubscription(sub *nats.Subscription, callbacks *callbackGate, pump *subscriptionPump) {
	_ = sub.Unsubscribe()
	callbacks.stopAndWait()
	// Every callback has exited, so nothing can enqueue past this point;
	// releasing the pump here lets it unwind whatever the queue still holds.
	pump.halt()
}

func (s *concurrentDurableSubscriber) terminateMalformed(msg *nats.Msg, subject string, decodeErr error) {
	s.log.Warn("terminating malformed NATS delivery", zap.String("subject", subject), zap.Error(decodeErr))
	if err := msg.Term(); err != nil {
		s.log.Warn("malformed NATS delivery TERM failed", zap.String("subject", subject), zap.Error(err))
	}
}

func messageFromNATS(wire *nats.Msg) (*Message, error) {
	metadata, err := fleetMetadata(wire.Header)
	if err != nil {
		return nil, err
	}
	return newMessage(messageData{
		id:       messageIdentity(wire),
		payload:  wire.Data,
		metadata: metadata,
	}), nil
}

// fleetMetadata copies the non-identity headers into delivery metadata. A
// delivery carrying only identity headers — every firehose event before trace
// propagation attaches NewRelic headers — returns a nil Metadata rather than
// an empty map: the unconditional allocation sat on the consume hot path, and
// readers go through Metadata.Get, whose nil-receiver index is Go's guaranteed
// zero-value read. Nothing in the fleet writes to delivered metadata (a Set on
// a nil map panics); callers needing a writable map build their own.
func fleetMetadata(headers nats.Header) (Metadata, error) {
	var metadata Metadata
	for key, values := range headers {
		switch key {
		case MessageIDHeader,
			nats.MsgIdHdr, nats.ExpectedLastMsgIdHdr, nats.ExpectedStreamHdr,
			nats.ExpectedLastSubjSeqHdr, nats.ExpectedLastSeqHdr:
			continue
		}
		if len(values) != 1 {
			return nil, fmt.Errorf("bus: multiple values in NATS header %q: %v", key, values)
		}
		if metadata == nil {
			metadata = make(Metadata, len(headers))
		}
		metadata[key] = values[0]
	}
	return metadata, nil
}

func messageIdentity(wire *nats.Msg) string {
	if id := wire.Header.Get(MessageIDHeader); id != "" {
		return id
	}
	if metadata, err := wire.Metadata(); err == nil && metadata.Sequence.Stream > 0 {
		return jetStreamIdentity(metadata.Domain, metadata.Stream, metadata.Sequence.Stream)
	}
	// This path covers legacy/core messages without JetStream reply metadata.
	// NUID is process-safe and avoids introducing UUID machinery.
	return nuid.Next()
}

// jetStreamIdentity is the fallback identity for an event whose publisher set
// none. It is derived rather than random so it survives a retry hop: the pull
// adapter stamps it from the pull API's own metadata (see pullWireMessage),
// which cannot reach nats.go's subscription-bound parser, and both paths must
// produce the same string for the same delivery.
func jetStreamIdentity(domain, stream string, sequence uint64) string {
	return fmt.Sprintf("js:%s:%s:%d", domain, stream, sequence)
}

// resultWatch reconciles one delivery without parking a goroutine or a timer
// on it.
//
// The resolve callback runs the acknowledgement on the worker goroutine that
// finished the handler, and the subscriber's shared keep-alive wheel covers
// the slow-handler case: each pass reports InProgress (renewing the server's
// AckWait) until the coarse handlerDeadline, after which ownership is
// deliberately given up to AckWait redelivery. On the normal path the handler
// resolves inside one progress interval, so the whole mechanism costs one
// atomic store; the wheel sweeps the dead entry on its next pass — no timer
// registration, no timer-heap traffic, no cancellation per message. The
// finished flag is the single arbiter between a late resolve and the deadline:
// whoever swaps it first owns the acks slot, exactly one of them releases it.
type resultWatch struct {
	s       *concurrentDurableSubscriber
	natsMsg *nats.Msg
	// armEpoch is written once under the wheel lock at registration and
	// read-only afterwards; the wheel's lock provides the happens-before edge
	// the walk relies on. Watch age is derived per pass from the wheel epoch,
	// so unlike the per-message timers there is no mutable elapsed field to
	// serialize.
	armEpoch uint64
	finished atomic.Bool
}

// unwind releases a watch whose message never reached a worker: mark it
// finished so the wheel's next pass skips it, and give the ack seat straight
// back.
func (w *resultWatch) unwind() {
	if w.finished.CompareAndSwap(false, true) {
		w.s.acks.Done()
	}
}

func (w *resultWatch) resolve(acked bool) {
	if !w.finished.CompareAndSwap(false, true) {
		// The deadline already surrendered this delivery to AckWait; another
		// ACK or NAK here would address a message the server may have
		// redelivered elsewhere.
		return
	}
	defer w.s.acks.Done()
	select {
	case <-w.s.closeCh:
		return
	default:
	}
	if acked {
		w.s.ack(w.natsMsg)
		return
	}
	w.s.nack(w.natsMsg)
}

// keepAliveWheel replaces the per-message time.AfterFunc the result watches
// used to arm. Arming and stopping a timer per delivery pushed every message
// through the runtime timer heap at lane rate for a mechanism that usually
// does nothing: handlers resolve inside one progress interval, so nearly all
// of that traffic was setup and teardown. The wheel arms nothing per message.
// Watches join the bucket the next pass will walk, and ONE ticker goroutine
// per subscriber walks one bucket per progress interval, reporting InProgress
// for every unfinished watch and sweeping the ones a resolve or unwind has
// marked finished — resolution never touches the bucket, the walk drops the
// corpse, so the common path pays nothing beyond the atomic mark.
//
// The ring has wheelStepCount(handlerDeadline, progress) buckets, and watch
// age is computed from wheel epochs rather than stored: at age >= steps the
// watch surrenders the delivery to AckWait exactly where the per-message
// timers gave up. Past the deadline no further progress is reported, because
// the server may already have redelivered to another replica and renewing
// would reclaim the ownership the deadline exists to give up.
type keepAliveWheel struct {
	interval time.Duration
	steps    int

	mu      sync.Mutex
	buckets [][]*resultWatch
	cursor  int
	epoch   uint64

	// done is the subscriber's closeCh: both Close paths close it exactly
	// once, and keeping the wheel live through the ack drain preserves the old
	// timers' behaviour of surrendering stuck deliveries while draining.
	done <-chan struct{}
}

func newKeepAliveWheel(steps int, interval time.Duration, done <-chan struct{}) *keepAliveWheel {
	if steps < 1 {
		steps = 1
	}
	w := &keepAliveWheel{
		interval: interval,
		steps:    steps,
		buckets:  make([][]*resultWatch, steps),
		done:     done,
	}
	go w.spin()
	return w
}

// register files a watch into the bucket the next pass walks. Its armEpoch is
// the epoch the wheel has completed, so the first pass that sees it computes
// age 1 and reports progress, matching the per-message timers' first fire one
// full progress interval after arming.
func (w *keepAliveWheel) register(watch *resultWatch) {
	w.mu.Lock()
	watch.armEpoch = w.epoch
	w.buckets[w.cursor] = append(w.buckets[w.cursor], watch)
	w.mu.Unlock()
}

func (w *keepAliveWheel) spin() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.advance()
		case <-w.done:
			return
		}
	}
}

// advance walks the bucket due this interval. The slice is detached under the
// lock and processed outside it: reportProgress is network round-trip work and
// must not serialize registrations happening mid-pass.
func (w *keepAliveWheel) advance() {
	w.mu.Lock()
	due := w.buckets[w.cursor]
	w.buckets[w.cursor] = nil
	w.cursor = (w.cursor + 1) % len(w.buckets)
	w.epoch++
	e := w.epoch
	next := w.buckets[w.cursor]
	w.mu.Unlock()

	var survivors []*resultWatch
	for _, watch := range due {
		if w.sweepDue(e, watch) {
			survivors = append(survivors, watch)
		}
	}
	w.refile(next, survivors)
}

// sweepDue reports one unfinished watch against its deadline. It returns false
// when the watch leaves the wheel: resolved or unwound since it was filed
// (lazy sweep), or past deadline, where it surrenders the delivery to AckWait.
// That surrender's CAS is the same arbiter resolve() uses, so exactly one side
// releases the acks seat.
func (w *keepAliveWheel) sweepDue(epoch uint64, watch *resultWatch) bool {
	if watch.finished.Load() {
		return false
	}
	if epoch-watch.armEpoch < uint64(w.steps) {
		watch.s.reportProgress(watch.natsMsg)
		return true
	}
	if watch.finished.CompareAndSwap(false, true) {
		watch.s.acks.Done()
	}
	return false
}

// refile returns survivors to the bucket the cursor points at: they are due
// again next interval, and registrations landing mid-pass joined the same
// slice under the same lock.
func (w *keepAliveWheel) refile(base []*resultWatch, survivors []*resultWatch) {
	if len(survivors) == 0 {
		return
	}
	w.mu.Lock()
	w.buckets[w.cursor] = append(base, survivors...)
	w.mu.Unlock()
}

// ack applies the acknowledgement contract the stream's retention actually
// needs. On a limits/interest stream the ACK only advances a cursor, so it is
// fired asynchronously: the double-ack proved the cursor had moved at the cost
// of a round trip per message, which becomes a RAFT quorum round trip per
// message once the lane's stream is replicated. Handlers are idempotent by
// contract (ADR 0003), so a lost ACK there only risks one redelivery after
// AckWait, which is safe; a stalled quorum on every ACK is not.
func (s *concurrentDurableSubscriber) ack(msg *nats.Msg) {
	if s.ackSync {
		s.ackWorkQueue(msg)
		return
	}
	if err := msg.Ack(); err != nil {
		s.log.Warn("durable message ack failed; leaving redelivery to AckWait",
			zap.String("subject", msg.Subject), zap.Error(err))
	}
}

// ackWorkQueue confirms the acknowledgement reached the server. Work-queue
// retention deletes the message on ack, so an ACK that never lands is not a
// stale cursor but a redelivery of work that already ran — on TWITCH_OUTGRESS
// that is the same chat line sent twice. A failed confirmation NAKs instead, so
// the redelivery is deliberate and paced rather than an AckWait surprise; if the
// ACK did land and only its reply was lost, the NAK addresses a message the
// server has already removed and does nothing.
func (s *concurrentDurableSubscriber) ackWorkQueue(msg *nats.Msg) {
	if err := msg.AckSync(nats.AckWait(ackSyncTimeout)); err != nil {
		s.log.Warn("work-queue message ack was not confirmed; nacking to force a paced redelivery",
			zap.String("subject", msg.Subject), zap.Error(err))
		s.nack(msg)
	}
}

func (s *concurrentDurableSubscriber) reportProgress(msg *nats.Msg) {
	// Slow RPC-backed commands retain ownership. The normal path never reaches
	// this ticker because processing finishes in well under one second.
	if err := msg.InProgress(); err != nil {
		s.log.Warn("durable message progress ack failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) nack(msg *nats.Msg) {
	delay := time.Duration(0)
	if s.delay != nil {
		if metadata, err := msg.Metadata(); err == nil {
			delay = s.delay.WaitTime(metadata.NumDelivered)
		}
	}
	var err error
	switch {
	case delay == terminateDelivery:
		err = msg.Term()
	case delay > 0:
		err = msg.NakWithDelay(delay)
	default:
		err = msg.Nak()
	}
	if err != nil {
		s.log.Warn("durable message NAK failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) Close() error {
	subs, started := s.beginClose()
	if !started {
		return nil
	}

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	s.stopCallbacks(subs)

	s.acks.Done() // no callback can Add after the drain barrier
	if !waitGroupBefore(&s.acks, deadline.C) {
		return s.abortClose(errors.New("bus: timed out draining durable acknowledgements"))
	}

	close(s.closeCh)
	if err := s.nc.FlushTimeout(2 * time.Second); err != nil {
		s.nc.Close()
		return fmt.Errorf("bus: flush subscriber acknowledgements: %w", err)
	}
	s.nc.Close()
	return nil
}

type ownedSubscription struct {
	sub       *nats.Subscription
	callbacks *callbackGate
	pump      *subscriptionPump
}

func (s *concurrentDurableSubscriber) beginClose() ([]ownedSubscription, bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, false
	}
	s.closed = true
	s.mu.Unlock()

	// beginRegistration serializes Add with the closed flag under mu, so once
	// closed is set no registration can race this Wait.
	s.registrations.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	subs := make([]ownedSubscription, 0, len(s.subs))
	for _, owned := range s.subs {
		subs = append(subs, owned)
	}
	return subs, true
}

func (s *concurrentDurableSubscriber) stopCallbacks(subs []ownedSubscription) {
	for _, owned := range subs {
		if err := owned.sub.Unsubscribe(); err != nil && !errors.Is(err, nats.ErrBadSubscription) {
			s.log.Warn("durable subscription stop failed", zap.String("subject", owned.sub.Subject), zap.Error(err))
		}
		stopSubscription(owned.sub, owned.callbacks, owned.pump)
	}
}

func waitGroupBefore(group *sync.WaitGroup, deadline <-chan time.Time) bool {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-deadline:
		return false
	}
}

func (s *concurrentDurableSubscriber) abortClose(err error) error {
	s.nc.Close()
	close(s.closeCh)
	return err
}
