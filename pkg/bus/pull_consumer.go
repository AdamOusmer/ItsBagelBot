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

	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// The shared-durable pull lane is the scale-OUT counterpart to the flow lane.
//
// The flow consumer cannot be shared: AckFlowControl answers a window with one
// cumulative floor, so a queue group would let one arbitrary member acknowledge
// work still running on every other member (flow_consumer.go). Its consequence
// is that every pod owns a consumer and therefore receives the WHOLE lane —
// under an autoscaler that is fan-out, not scale-out: N pods cost N deliveries
// off the stream leader and N handler executions per event, and only the
// idempotency guard keeps the duplicates from being acted on.
//
// A pull consumer inverts the acknowledgement problem. The server hands a batch
// to whichever pod asked for it, so ONE durable serves the whole fleet and each
// message is delivered to exactly one pod. There is no delivery subject, no
// heartbeat conversation and no per-pod cursor to keep coherent, so the durable
// deliberately carries no pod identity at all.
//
// The trade this makes explicit: AckAll on a shared durable means a floor ack
// published by one pod covers every sequence at or below it, including messages
// another pod is still holding. That is the same receipt-level contract the flow
// lane already runs under (a hard process loss can lose the in-flight window),
// widened from one pod to the fleet. Handlers must stay idempotent and
// replay-sensitive control lanes must stay on explicit ACK, exactly as before.
const (
	// defaultPullFetchBatch is how many messages one pull request asks for and
	// the receipt cadence of the floor ack. Batching is the whole economy of
	// the pull path: one request and one floor ack cover the batch, so the
	// per-message API cost falls as the batch grows. The continuous iterator
	// keeps a refill for this many in flight while the current buffer is
	// dispatched, and MaxAckPending must stay comfortably above twice this.
	defaultPullFetchBatch = 1000

	// defaultPullFetchLoops is how many continuous-pull iterators one pod runs
	// against the ONE shared durable. A server distributes batches across every
	// open iterator context on a durable, so N loops give a pod N concurrent
	// batches in flight — drain parallelism scales with the knob without
	// multiplying consumers or deliveries. The ceiling it spends is fleet-wide
	// in-flight (MaxAckPending must exceed pods x loops x batch); the 50k
	// default dwarfs any sane loops x batch product.
	defaultPullFetchLoops = 1

	// defaultPullFetchMaxWait bounds an unfilled batch. It is the lane's idle
	// latency floor and the loop's own spin rate, so it is short enough that a
	// trickling lane is not held for a fill that is not coming.
	defaultPullFetchMaxWait = 200 * time.Millisecond

	// defaultPullAckWait is generous on purpose. It is the server's redelivery
	// clock for anything the floor has not yet covered, and the floor moves on
	// the batch/timer cadence below rather than per message — an AckWait near
	// that cadence would redeliver a batch this pod is mid-way through handing
	// out, to a different pod, for no reason.
	defaultPullAckWait = 30 * time.Second

	// defaultPullMaxAckPending is the fleet-wide in-flight ceiling, not a
	// per-pod one: a shared durable has a single pending set that every pod
	// draws from. It must exceed (pods x batch x fetches in flight) or the
	// server stops handing out batches and the lane stalls below its rate.
	defaultPullMaxAckPending = 50_000

	// defaultPullAckEvery advances the floor while a batch is still being handed
	// to handlers. Without it a pod that blocks on a slow handler pool holds the
	// whole batch unacked for as long as the pool is busy, and the shared pending
	// set — which every other pod is drawing from — shrinks by that much.
	defaultPullAckEvery = 250 * time.Millisecond

	// defaultPullReplicas matches the lane streams' own replica count. The durable
	// has to survive exactly what the stream survives, and a consumer cannot ask
	// for more replicas than the stream it reads has.
	defaultPullReplicas = 3

	// laneUnhealthyAfter is how long the fetch loop may stay in its error path
	// before the lane reports itself unready. It has to clear an ordinary consumer
	// leader election and a client reconnect — both error for a few seconds — and
	// stay far under the time anyone would take to notice the silence by hand.
	laneUnhealthyAfter = 30 * time.Second

	pullProvisionTimeout = 5 * time.Second
)

// pullConsumerConfig is the one durable the whole fleet binds. Everything about
// it is chosen so that adding a pod adds throughput rather than copies.
//
// AckAll rather than AckExplicit: the acknowledgement this lane needs is a
// floor, and a floor costs one publish per batch instead of one per message. On
// a hot lane that difference is the difference between an ack stream the leader
// can absorb and one it cannot.
//
// DeliverNew applies only to a first-ever creation. The lane stream retains
// minutes of firehose, so a durable created at DeliverAll would open by
// replaying every retained event; ensurePullConsumer echoes the server's own
// start position on every later provision, which is what makes the second pod's
// boot an update rather than a rejection.
func pullConsumerConfig(subject, name string) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:          name,
		Durable:       name,
		Description:   "ItsBagelBot shared-durable pull lane consumer",
		DeliverPolicy: jsapi.DeliverNewPolicy,
		AckPolicy:     pullAckPolicy(),
		AckWait:       pullAckWait(),
		MaxDeliver:    -1,
		FilterSubject: subject,
		ReplayPolicy:  jsapi.ReplayInstantPolicy,
		MaxAckPending: pullMaxAckPendingFor(pullAckPolicy()),
		// The durable outlives any single pod, so this only fires once the WHOLE
		// fleet has stopped fetching from it. It is the same window the flow lane
		// gives a pod to reconnect inside without losing its position.
		InactiveThreshold: flowInactiveThreshold,
		// Consumer state is replicated with the stream it reads.
		//
		// This was R1, on the reasoning that per-consumer ack state is leader RAFT
		// work on the hot path and that the fleet would simply re-provision after
		// losing it. The second half was never true: nothing rebuilt a lane durable,
		// so a single peer owned the whole fleet's ability to consume. On 2026-08-16
		// one member lost peer routing for 23 minutes, the meta layer moved these
		// durables eleven times while it churned and then lost them, and every pod
		// spun on a name that no longer resolved for the next seven hours.
		// Quorum-replicated state survives one member, which is the failure that
		// actually happens; rebuildConsumer covers losing all of them at once.
		//
		// Memory storage stays. The lane streams are memory-backed themselves, so
		// persisting ack state to disk cannot outlive the messages it describes.
		Replicas:      pullReplicas(),
		MemoryStorage: true,
		Metadata:      map[string]string{managedConsumerMetadata: "true"},
	}
}

// pullConsumerName is deliberately the plain fleet-wide durable name, with no
// pod identity appended. That absence is the feature: every pod binding the same
// consumer is what makes the server distribute the lane instead of copying it,
// and a pod suffix here would silently turn this back into the fan-out shape the
// flow consumer is stuck with.
func pullConsumerName(group, subject string) string {
	return durableName(group, subject)
}

// pullAckPolicy is AckNone unless NATS_PULL_ACK_POLICY=all.
//
// The durable stays replicated (R3): its config and delivered cursor survive
// a member loss and a new leader resumes from the replicated position. What
// AckNone changes is one thing in nats-server 2.14.4: with AckAll or
// AckExplicit an R>1 consumer PARKS every delivery until its own RAFT group
// has committed the delivered-state proposal (consumer.go:5714
// replicateDeliveries, :5683 addReplicatedQueuedMsg); with AckNone the cursor
// is still proposed (consumer.go:2985) but the message is sent at once.
//
// The floor ack it gives up bought redelivery only for messages the stream
// still holds when AckWait (30s) expires, and every hot lane expires its
// messages long before that (TWITCH_INGRESS MaxAge 10s, streams.go): after a
// pod loss the window is gone either way, which is the contract the lane
// already documents, and a failover replays the few deliveries between the
// old and new leaders' cursors, covered by the idempotency contract.
//
// Measured on the production hub, 2026-09-01, same publisher shape at 90k
// msg/s: R3 durable with AckAll e2e p50 18 ms / p99 603 ms; R3 with AckNone
// p50 1.8-2.9 ms / p99 16-19 ms. Chosen as the default for that reason; "all"
// stays selectable for a lane whose retention outlives AckWait.
func pullAckPolicy() jsapi.AckPolicy {
	if env.Get("NATS_PULL_ACK_POLICY", "none") == "all" {
		return jsapi.AckAllPolicy
	}
	return jsapi.AckNonePolicy
}

// pullMaxAckPendingFor is zero under AckNone: nats-server 2.14.4 refuses a
// MaxAckPending on an AckNone consumer (consumer.go:803), so the ceiling is
// only sent when there is pending state for it to bound.
func pullMaxAckPendingFor(policy jsapi.AckPolicy) int {
	if policy == jsapi.AckNonePolicy {
		return 0
	}
	return pullMaxAckPending()
}

func pullAckWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_ACK_WAIT", defaultPullAckWait), defaultPullAckWait)
}

func pullAckEvery() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_ACK_EVERY", defaultPullAckEvery), defaultPullAckEvery)
}

func pullFetchMaxWait() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_FETCH_MAXWAIT", defaultPullFetchMaxWait), defaultPullFetchMaxWait)
}

func pullMaxAckPending() int {
	return positiveInt(env.GetInt("NATS_PULL_MAX_ACK_PENDING", defaultPullMaxAckPending), defaultPullMaxAckPending)
}

func pullFetchBatch() int {
	return positiveInt(env.GetInt("NATS_PULL_FETCH_BATCH", defaultPullFetchBatch), defaultPullFetchBatch)
}

// pullFetchExpiry is the pull request expiry, nats.go's own 30s default unless
// NATS_PULL_EXPIRY says otherwise. It is a latency knob in disguise: nats.go
// (jetstream/pull.go, Messages defaults) arms an idle-heartbeat monitor only
// when the expiry is 10s or more, and Next then creates and stops one
// time.AfterFunc PER MESSAGE for that monitor. An expiry under 10s turns the
// heartbeat off and with it two timer-heap operations and an allocation on
// every delivery; the cost is that a dead server is noticed by the request
// expiring rather than by a missed heartbeat, which the fetch-error path
// already handles at the same cadence.
//
// Measured 2026-09-01 (loopback 3-node 2.14.4, R1 stream, 150k msg/s, 8 pull
// loops): consumer CPU 8.3-8.4 µs/msg at 5s against 6.9-8.4 µs/msg at 30s, so
// the timer is not where the consumer's cost is.
//
// What the expiry does bound is a startup race, measured the same day on an R3
// stream with an R3 durable: two pods that bind the shared durable at the same
// instant leave one of them with its first pull requests queued on the server
// but never served — consumer INFO counts them in num_waiting, nothing is
// delivered, no status message reaches the client, and pkg/bus reports the
// lane healthy because a blocked Next is indistinguishable from an idle lane.
// The starved pod recovers only when those requests expire: 28 s of nothing
// at the 30 s default, 3-5 s at 5 s, and none at all when the second bind
// comes 2 s after the first. The default stays 30 s here because changing it
// changes every deployment at once; NATS_PULL_EXPIRY=5s is the bounded-stall
// setting until the race itself is closed.
func pullFetchExpiry() time.Duration {
	return positiveDuration(env.GetDuration("NATS_PULL_EXPIRY", 30*time.Second), 30*time.Second)
}

func pullFetchLoops() int {
	return positiveInt(env.GetInt("NATS_PULL_FETCH_LOOPS", defaultPullFetchLoops), defaultPullFetchLoops)
}

// pullReplicas exists as a knob only so a single-node broker (local runs, the
// acceptance rigs) can bind the lane at all: asking three replicas of a
// one-member cluster is a creation the server refuses, and the lane binding is
// a boot-fatal step. Production never sets it.
func pullReplicas() int {
	return positiveInt(env.GetInt("NATS_PULL_REPLICAS", defaultPullReplicas), defaultPullReplicas)
}

// positiveDuration and positiveInt refuse a zero or negative override. Every
// knob here is a rate or a ceiling, and the server rejects or silently reshapes
// a non-positive one — a typo in a manifest must fall back to the shipped
// default rather than change the consumer's shape.
func positiveDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func positiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// pullConsumerProvisioner is the slice of the JetStream API this binding uses.
// Narrow for the same reason streamProvisioner is: the destructive verb is
// visible in the type rather than buried in a call, and the tests can drive the
// replacement path without a broker.
type pullConsumerProvisioner interface {
	Consumer(ctx context.Context, stream, name string) (jsapi.Consumer, error)
	PushConsumer(ctx context.Context, stream, name string) (jsapi.PushConsumer, error)
	CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jsapi.ConsumerConfig) (jsapi.Consumer, error)
	DeleteConsumer(ctx context.Context, stream, name string) error
}

// pullLeaderPoll is the spacing between the two agreeing INFO reads
// awaitPullLeader needs; a consumer election takes longer than this.
const pullLeaderPoll = 150 * time.Millisecond

// pullDelivery pairs a decoded message with the core message the shared helpers
// speak, so the retry path can re-send the original bytes and headers without
// re-encoding them.
type pullDelivery struct {
	wire *nats.Msg
	msg  *Message
}

// pullSubscriber is the shared-durable delivery adapter. NATS_PULL_FETCH_LOOPS
// fetch loops per pod feed one lane channel; every consumer unit in the process
// reads that channel, so the pod keeps exactly one binding however far the
// routine autoscaler grows, and the fleet keeps exactly one consumer however far
// the pod autoscaler grows.
//
// There is no receipt queue here, unlike the flow adapter. Nothing is being
// pushed: the loop asks for the next batch only once it has handed the previous
// one out, so a slow handler pool is backpressure onto the server's pending set
// rather than a queue this process has to size against a flow-control window.
type pullSubscriber struct {
	nc *nats.Conn
	// consumer is read by every fetch loop and replaced only inside
	// rebuildConsumer, which consumerMu both locks and serializes — with more
	// than one loop, any pump may be the one to notice the durable is gone.
	consumer jsapi.Consumer
	// desired is the shape rebuildConsumer re-provisions, kept so a rebuild can
	// never drift from the binding this loop was started with.
	desired jsapi.ConsumerConfig
	// rebind provisions a replacement durable. It is a field rather than a direct
	// call so the rebuild decision is testable without a broker; the constructor
	// always fills it with the real API path.
	rebind func() (jsapi.Consumer, error)
	// extra are the additional fetch connections NATS_PULL_CONNECTIONS asks for,
	// each with its own lookup-bound handle on the same durable; see
	// pullConnections for the measurement behind them.
	extra    []*nats.Conn
	handles  []jsapi.Consumer
	handleMu sync.Mutex
	stream   string
	subject  string
	name     string
	log      *zap.Logger

	batch    int
	loops    int
	maxWait  time.Duration
	ackEvery time.Duration

	retried   atomic.Int64
	dropped   atomic.Int64
	fetchErrs atomic.Int64
	ackErrs   atomic.Int64
	rebuilt   atomic.Int64
	closed    atomic.Bool

	// errSince is the unix-nano instant the fetch loop entered its error path, or
	// zero while it is healthy. An idle lane never sets it — Next simply blocks —
	// so silence and sickness stay distinguishable to Healthy.
	errSince atomic.Int64

	// ackMu guards the newest delivery the floor has not yet covered. Both the
	// fetch loops and the ack timer publish the floor, and two acks racing would
	// otherwise be free to move it backwards.
	ackMu   sync.Mutex
	pending jsapi.Msg

	// consumerMu guards consumer across the fetch loops and serializes their
	// rebuild attempts.
	consumerMu sync.Mutex

	output  chan *Message
	closeCh chan struct{}
	// ctx is the lane's own delivery context, carried on every message. Units come
	// and go around the shared binding, so a single unit's context cannot own the
	// cancellation every handler reads; this one is cancelled when the binding
	// itself is released.
	ctx    context.Context
	cancel context.CancelFunc

	workers  sync.WaitGroup
	inflight sync.WaitGroup
	once     sync.Once
}

// As with the flow lane, there is deliberately no exported constructor that
// names a stream and subject directly: NewSubscriber is the only entry point, so
// the catalog and the hot-lane scope guard keep deciding which lanes get
// receipt-level acknowledgement.

func newPullLaneSubscriber(cfg flowLaneConfig) (*pullSubscriber, error) {
	name := pullConsumerName(cfg.group, cfg.subject)

	// The connection name separates the acknowledgement contracts in NATS
	// monitoring, so a mixed rollout is legible from connz alone.
	nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(cfg.group)+"-pull")...)
	if err != nil {
		return nil, err
	}
	consumer, err := ensurePullConsumer(nc, cfg.stream, pullConsumerConfig(cfg.subject, name))
	if err != nil {
		nc.Close()
		return nil, err
	}
	s := newPullSubscriber(cfg, nc, consumer, name)
	if err := s.openExtraConnections(cfg); err != nil {
		nc.Close()
		return nil, err
	}
	s.start()
	return s, nil
}

func newPullSubscriber(cfg flowLaneConfig, nc *nats.Conn, consumer jsapi.Consumer, name string) *pullSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	loops, batch := pullFetchLoops(), pullFetchBatch()
	s := &pullSubscriber{
		nc: nc, consumer: consumer, desired: pullConsumerConfig(cfg.subject, name),
		stream: cfg.stream, subject: cfg.subject,
		name: name, log: log,
		batch: batch, loops: loops, maxWait: pullFetchMaxWait(), ackEvery: pullAckEvery(),
		// One slot per batch every loop can hold at once: a full channel is
		// backpressure onto the server's pending set, never a dropped refill
		// and never a serialization point between the loops.
		output:  make(chan *Message, loops*batch),
		closeCh: make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
	s.rebind = func() (jsapi.Consumer, error) { return ensurePullConsumer(s.nc, s.stream, s.desired) }
	return s
}

// start runs the floor-ack timer plus one pump per configured fetch loop. The
// pumps share the single jsapi.Consumer: each opens its own continuous iterator
// and the server hands each context its own batch off the same pending set, so
// the loops never double-deliver to this pod.
func (s *pullSubscriber) start() {
	s.workers.Add(s.loops + 1)
	for i := range s.loops {
		go s.pump(i)
	}
	go s.advanceFloorPeriodically()
}

// Subscribe hands back the pod's single lane channel. Every consumer unit reads
// the same channel, which is what keeps one fetch loop per pod while the unit
// autoscaler grows and shrinks.
func (s *pullSubscriber) Subscribe(_ context.Context, subject string) (<-chan *Message, error) {
	if subject != s.subject {
		return nil, fmt.Errorf("bus: pull subscriber is bound to %q, not %q", s.subject, subject)
	}
	if s.closed.Load() {
		return nil, errors.New("bus: subscriber is closed")
	}
	return s.output, nil
}

func (s *pullSubscriber) pump(i int) {
	defer s.workers.Done()
	for s.pumpIterator(i) {
	}
}

// pumpIterator opens one continuous-pull iterator and drains it until shutdown
// or a transport error. The iterator keeps a refill request in flight while the
// current buffer is being dispatched, so handlers no longer sit idle for a
// fetch round trip between batches the way the batch-by-batch loop did — under
// load that bubble was a fixed RTT out of every batch's wall clock. The floor
// still advances on the receipt cadence: every s.batch receipts here, plus the
// timer in advanceFloorPeriodically for a buffer that is only partially handed
// out.
func (s *pullSubscriber) pumpIterator(i int) bool {
	iter, err := s.handleFor(i).Messages(jsapi.PullMaxMessages(s.batch), jsapi.PullExpiry(pullFetchExpiry()))
	if err != nil {
		return s.noteFetchError(err)
	}
	release := s.stopIteratorOnClose(iter)
	defer release()
	// A successfully bound iterator IS fetch-loop progress: Next may legitimately
	// block for hours on an idle lane. Clearing the health clock here — not only
	// on message arrival — is what lets a lane recover from one transient error
	// while idle; otherwise errSince ages past laneUnhealthyAfter and the pod
	// reports itself unready until traffic happens to return.
	s.noteFetchProgress()
	for {
		wire, err := iter.Next()
		if err != nil {
			// Every error here is real. An idle lane does NOT surface one: the
			// 408 that ends an unfilled pull request is a status message
			// nats.go consumes itself (jetstream/pull.go handleStatusMsg), so
			// Next simply blocks until the next message or a genuine failure.
			// There is no idle case to special-case, and treating one as
			// benign skips both the backoff and the health clock below.
			//
			// Cover everything already handed out before deciding whether the
			// error is shutdown or a transport failure worth backing off on.
			s.advanceFloor()
			if s.closed.Load() {
				return false
			}
			return s.noteFetchError(err)
		}
		s.noteFetchProgress()
		if !s.deliver(wire) {
			return false
		}
		// Floor advancement is deliberately timer-driven ONLY. Every advance is
		// a consumer-group RAFT proposal serialized against fetch serving on the
		// leader; measured on the canary lane, per-batch triggering capped
		// aggregate drain near 55-60k msg/s while a never-ack control ran 82k.
		// AckWait (30s) dwarfs the worst-case coverage delay of the 250ms
		// advanceFloorPeriodically cadence, so batching receipts into the timer
		// trades nothing but a bounded ack-floor lag.
	}
}

// boundConsumer snapshots the consumer handle a new iterator will open. A pump
// keeps the handle it opened even if a concurrent rebuild swaps the binding —
// the stale handle fails on its own terms and that pump's next fetch error
// rebuilds again.
func (s *pullSubscriber) boundConsumer() jsapi.Consumer {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()
	return s.consumer
}

// stopIteratorOnClose unparks a blocked Next the moment the binding closes.
// The returned release tears the watcher down when the iterator ends first;
// stopping twice is safe. Every iterator gets its own watcher, so N pumps each
// own their iterator and its release outright.
func (s *pullSubscriber) stopIteratorOnClose(iter jsapi.MessagesContext) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-s.closeCh:
		case <-done:
		}
		iter.Stop()
	}()
	return func() { close(done) }
}

// deliver hands one message to the lane channel and records it as the newest
// receipt the floor has not yet covered. A malformed payload is recorded too: it
// WAS received, and leaving a hole in the floor for it would hold the shared
// pending set open against every pod on the lane.
//
// The verdict is reconciled through a resolve callback rather than a goroutine
// parked on Acked/Nacked, for the reason spelled out on the flow lane's deliver:
// at lane rate that goroutine and its timer were pure overhead for an outcome
// that usually does nothing. An ACK is receipt-level here — the batch floor
// already owns it — and only a NACK costs anything, one scheduled retry, now run
// on the handler's own goroutine. A delivery nobody resolves holds its inflight
// count until shutdown bounds the wait at subscriberDrainTimeout.
func (s *pullSubscriber) deliver(wire jsapi.Msg) bool {
	delivery, ok := s.decode(wire)
	if !ok {
		s.noteReceipt(wire)
		return true
	}
	// Both the count and the handler are in place before the message is visible
	// to a handler: it can be resolved the instant the send completes.
	s.inflight.Add(1)
	delivery.msg.setResolveHandler(func(acked bool) {
		defer s.inflight.Done()
		defer pullEnvelopePool.Put(delivery.wire)
		if !acked {
			s.scheduleRetry(delivery)
		}
	})
	select {
	case s.output <- delivery.msg:
		s.noteReceipt(wire)
		return true
	case <-s.closeCh:
		// Nobody took the message, so the callback will never run and the count
		// it owns has to be released here or shutdown waits out its whole budget.
		// The resolve handler owning the envelope's Put never fires for the same
		// reason — the message was never handed off — so return it here.
		s.inflight.Done()
		pullEnvelopePool.Put(delivery.wire)
		return false
	}
}

func (s *pullSubscriber) decode(wire jsapi.Msg) (pullDelivery, bool) {
	core := pullWireMessage(wire)
	msg, err := messageFromNATS(core)
	if err != nil {
		// The resolve handler that returns the envelope never exists on this
		// path — nothing was delivered — so the pool gets it back here.
		pullEnvelopePool.Put(core)
		s.log.Warn("dropping malformed lane delivery",
			zap.String("subject", s.subject), zap.Error(err))
		return pullDelivery{}, false
	}
	msg.SetContext(s.laneContext())
	return pullDelivery{wire: core, msg: msg}, true
}

// pullEnvelopePool recycles the per-delivery *nats.Msg envelope that
// pullWireMessage rebuilds for the shared decode and retry helpers. The
// envelope never outlives its Message's resolution: messageFromNATS copies the
// identity and metadata out and leaves only a Data alias on the delivered
// Message (owned by the fetch batch, untouched here), and the resolve handler —
// which fires exactly once, after the handler finishes — is the last reader
// before Put returns it. Data is deliberately not cleared on Put: the acquirer
// overwrites every field, and the old buffer may still be aliased by a delivered
// Message's payload.
var pullEnvelopePool = sync.Pool{New: func() any { return new(nats.Msg) }}

// pullWireMessage rebuilds the core message the shared decode and retry helpers
// speak.
//
// The identity has to be stamped here rather than left to messageIdentity's
// JetStream fallback: that fallback reads delivery metadata through nats.go's
// own subscription binding, and a message rebuilt from the pull API has no
// subscription to read it through. Without the stamp every ingress-origin event
// (which carries no publisher-set id) would mint a fresh NUID per delivery, and
// a retry hop would land under an id no dedup guard could match to the original.
func pullWireMessage(wire jsapi.Msg) *nats.Msg {
	core := pullEnvelopePool.Get().(*nats.Msg)
	core.Subject = wire.Subject()
	core.Reply = wire.Reply()
	core.Data = wire.Data()
	// The delivered header map is ours once nats.go hands the message over;
	// adopting it costs one pointer store where a reset-and-copy walked the
	// map twice per delivery.
	core.Header = wire.Headers()
	if core.Header == nil {
		core.Header = make(nats.Header, 1)
	}
	metadata, err := wire.Metadata()
	if err != nil || metadata.Sequence.Stream == 0 {
		return core
	}
	// Stamped after reset so a recycled envelope cannot carry a prior
	// delivery's identity forward.
	if core.Header.Get(MessageIDHeader) == "" {
		core.Header.Set(MessageIDHeader,
			jetStreamIdentity(metadata.Domain, metadata.Stream, metadata.Sequence.Stream))
	}
	return core
}

// laneContext keeps a directly-constructed subscriber (tests) usable without a
// context of its own.
func (s *pullSubscriber) laneContext() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// noteReceipt records the newest delivery the floor ack has not yet covered.
// Ordering inside one loop's batch is the server's, so within a single loop the
// last receipt recorded names the highest sequence that loop received.
//
// Across loops the last writer can name a LOWER sequence than a receipt another
// loop recorded but has not published yet. That cannot move the floor
// backwards: an AckAll ack is cumulative and the server's ack floor is
// monotonic, so publishing a sequence at or below an already-published floor is
// a no-op. What interleaving costs is coverage delay — the masked window above
// the lower sequence stays unacked until some later fresh batch, whose stream
// sequences all exceed anything this durable has delivered so far, publishes a
// receipt over it (the periodic timer alone cannot: it re-publishes the same
// masked pending). Worst case is redelivery of the masked window after AckWait,
// which the idempotency contract on this lane already tolerates.
func (s *pullSubscriber) noteReceipt(wire jsapi.Msg) {
	if s.desired.AckPolicy == jsapi.AckNonePolicy {
		return
	}
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	s.pending = wire
}

// advanceFloor publishes the cumulative ack, asynchronously and exactly once per
// pending receipt. It is never AckSync: a synchronous ack costs a quorum round
// trip per call on an R3 stream, which is the cost this whole lane shape exists
// to avoid.
func (s *pullSubscriber) advanceFloor() {
	wire := s.takePending()
	if wire == nil {
		return
	}
	if err := wire.Ack(); err != nil {
		// The floor is cumulative, so the next ack re-covers whatever this one
		// missed; a lost ack costs redelivery latency rather than the binding.
		if count := s.ackErrs.Add(1); count == 1 || count%1_000 == 0 {
			s.log.Warn("lane floor ack failed",
				zap.String("subject", s.subject),
				zap.Int64("ack_errors", count),
				zap.Error(err))
		}
	}
}

func (s *pullSubscriber) takePending() jsapi.Msg {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	wire := s.pending
	s.pending = nil
	return wire
}

// advanceFloorPeriodically covers a batch that is only partially handed out. The
// fetch loop acks at the end of a batch, so a pod blocked on a slow handler pool
// would otherwise hold every message it has already delivered unacked for as
// long as the pool is busy — and on a shared durable that pending set is the one
// every other pod is drawing from.
func (s *pullSubscriber) advanceFloorPeriodically() {
	defer s.workers.Done()
	ticker := time.NewTicker(s.ackEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.closeCh:
			return
		case <-ticker.C:
			s.advanceFloor()
		}
	}
}

// scheduleRetry is the same one-hop redelivery the flow lane uses, for the same
// reason: the floor has already moved past this message, so there is no
// per-message pending state left to NAK against.
func (s *pullSubscriber) scheduleRetry(delivery pullDelivery) {
	if err := scheduleLaneRetry(s.nc, s.subject, delivery.wire, delivery.msg); err != nil {
		s.log.Warn("dropping failed lane event",
			zap.String("subject", s.subject),
			zap.String("message_id", delivery.msg.UUID),
			zap.Int64("dropped_total", s.dropped.Add(1)),
			zap.Error(err))
		return
	}
	s.retried.Add(1)
}

// noteFetchError keeps a transient API failure from becoming a spin. A failed
// fetch returns immediately, so a leader election or a denied MSG.NEXT
// permission would otherwise burn a core re-requesting thousands of times a
// second. The pause is the fetch's own wait, which is the cadence the loop
// already runs at.
func (s *pullSubscriber) noteFetchError(err error) bool {
	if s.closed.Load() {
		return false
	}
	s.errSince.CompareAndSwap(0, time.Now().UnixNano())
	if count := s.fetchErrs.Add(1); count == 1 || count%1_000 == 0 {
		s.log.Warn("lane fetch failed",
			zap.String("stream", s.stream),
			zap.String("subject", s.subject),
			zap.String("consumer", s.name),
			zap.Int64("fetch_errors", count),
			zap.Error(err))
	}
	if consumerGone(err) {
		s.rebuildConsumer()
	}
	return s.pause(s.maxWait)
}

// noteFetchProgress clears the error clock the moment the loop reads a message
// again. It is a relaxed load per delivery on the hot path and a store only on
// the edge, so a healthy lane pays a comparison and nothing else.
func (s *pullSubscriber) noteFetchProgress() {
	if s.errSince.Load() != 0 {
		s.errSince.Store(0)
	}
}

// consumerGone separates "the durable this binding names no longer exists" from
// every other fetch failure. A leadership change, a timeout or a dropped
// connection is the lane working through something and the next fetch may well
// succeed; these two mean the name resolves to nothing and every later fetch
// fails identically until something recreates it.
func consumerGone(err error) bool {
	return errors.Is(err, jsapi.ErrConsumerNotFound) || errors.Is(err, jsapi.ErrConsumerDeleted)
}

// rebuildConsumer re-provisions the shared durable after the server has lost it.
//
// The lane durable is fleet-wide and genuinely deletable: InactiveThreshold
// expires it once the whole fleet stops fetching, and losing every replica of
// its state takes it with them. The original design accepted that and assumed
// "the fleet re-provisions" — but no code did, so a deleted durable meant every
// pod asking a name that no longer resolved, five times a second, until someone
// restarted the deployment by hand. This is that missing half.
//
// It runs on whichever fetch loop hit the error; consumerMu serializes the
// rebinds and the swap itself now that more than one loop can notice the loss
// at once. A second loop's rebuild against the fresh durable converges to a
// no-op update, so the serialization costs a wasted round trip, not churn.
func (s *pullSubscriber) rebuildConsumer() {
	// The old consumer's receipt can never be acked against a new one, and
	// holding it would spend the next floor ack on a guaranteed failure.
	s.takePending()

	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()
	consumer, err := s.rebind()
	if err != nil {
		s.log.Warn("lane consumer rebuild failed",
			zap.String("stream", s.stream),
			zap.String("subject", s.subject),
			zap.String("consumer", s.name),
			zap.Error(err))
		return
	}
	s.consumer = consumer
	s.relookupHandles()
	s.log.Warn("lane consumer was missing and has been rebuilt",
		zap.String("stream", s.stream),
		zap.String("subject", s.subject),
		zap.String("consumer", s.name),
		zap.Int64("rebuilds", s.rebuilt.Add(1)))
}

// Healthy reports whether this lane's fetch loop is making progress, which is
// the readiness signal a NATS connection check cannot give: a lane whose durable
// has been deleted keeps a perfectly healthy connection while consuming nothing.
// An idle lane is healthy — only sustained failure is not.
func (s *pullSubscriber) Healthy() bool {
	since := s.errSince.Load()
	return since == 0 || time.Since(time.Unix(0, since)) < laneUnhealthyAfter
}

func (s *pullSubscriber) pause(wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.closeCh:
		return false
	}
}

func (s *pullSubscriber) Close() error {
	var err error
	s.once.Do(func() { err = s.shutdown() })
	return err
}

// shutdown stops fetching, publishes the final floor, then drains. The order
// matters: the floor covers everything this pod took off the wire, and a pod
// that exits without publishing it hands the whole last batch back to the fleet
// for redelivery.
func (s *pullSubscriber) shutdown() error {
	s.closed.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	close(s.closeCh)
	s.workers.Wait()
	s.advanceFloor()

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	drained := waitGroupBefore(&s.inflight, deadline.C)

	err := s.flushAndClose(drained)
	// The fetch loops are the only senders and have exited with the workers.
	close(s.output)
	return err
}

// flushAndClose pushes the final floor ack and any last retry schedule onto the
// wire before the connection goes away. Closing without a flush discards
// whatever is still buffered, which here is exactly the acknowledgement that
// keeps the fleet from replaying this pod's last batch.
func (s *pullSubscriber) flushAndClose(drained bool) error {
	flushErr := s.nc.FlushTimeout(2 * time.Second)
	s.nc.Close()
	s.closeExtra()
	if !drained {
		return errors.New("bus: timed out draining pull lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush pull lane acknowledgements: %w", flushErr)
	}
	return nil
}
