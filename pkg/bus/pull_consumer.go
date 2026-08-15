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
		AckPolicy:     jsapi.AckAllPolicy,
		AckWait:       pullAckWait(),
		MaxDeliver:    -1,
		FilterSubject: subject,
		ReplayPolicy:  jsapi.ReplayInstantPolicy,
		MaxAckPending: pullMaxAckPending(),
		// The durable outlives any single pod, so this only fires once the WHOLE
		// fleet has stopped fetching from it. It is the same window the flow lane
		// gives a pod to reconnect inside without losing its position.
		InactiveThreshold: flowInactiveThreshold,
		// R1 consumer state on an R3 stream, per NATS maintainer guidance for
		// high-rate consumers: replicating per-consumer ack state is leader RAFT
		// work on the hot path, and this design does not need it. Losing the state
		// means the fleet re-provisions and resumes from the last replicated floor;
		// the idempotency guard absorbs the redelivered window and the stream's own
		// durability is untouched.
		Replicas:      1,
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
	CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jsapi.ConsumerConfig) (jsapi.Consumer, error)
	DeleteConsumer(ctx context.Context, stream, name string) error
}

// ensurePullConsumer provisions the fleet-wide durable and returns a handle to
// fetch from. Every pod runs this against the same name, so all but the first
// are updates.
func ensurePullConsumer(nc *nats.Conn, stream string, desired jsapi.ConsumerConfig) (jsapi.Consumer, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return nil, fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pullProvisionTimeout)
	defer cancel()
	return bindPullConsumer(ctx, js, stream, desired)
}

// bindPullConsumer converges the shared durable, replacing it only when the
// server has refused the update for a field it will never let an update change.
//
// The replacement path exists because the lane consumer's NAME is the same in
// every mode — durableName(group, subject), with no mode token — so flipping
// NATS_CONSUME_MODE to pull re-provisions the durable a push consumer is already
// occupying, and nats-server refuses that conversion in place. Without this the
// flip fails every pod's lane binding, permanently and identically, with the
// lane simply not consuming.
func bindPullConsumer(
	ctx context.Context,
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
) (jsapi.Consumer, error) {
	consumer, info, err := provisionPullConsumer(ctx, js, stream, desired)
	if err == nil {
		return consumer, nil
	}
	if !requiresConsumerReplacement(err) {
		return nil, fmt.Errorf("bus: provision pull consumer %q: %w", desired.Name, err)
	}

	// Re-drive once against whatever shape the durable has NOW, before anything
	// destructive. This is where the pull path differs from the flow path in
	// kind, not degree: a flow consumer is per-pod, so replacing one costs the
	// caller its own cursor, while this durable is fleet-wide and a delete takes
	// it out from under every other pod fetching from it. A rejection can simply
	// mean another pod completed the conversion between our INFO and our update,
	// and against the successor's shape the same request is an ordinary no-op —
	// so one wasted round trip here is what keeps a simultaneous fleet restart
	// from turning into one delete per pod, each destroying the last one's work.
	if consumer, _, raced := provisionPullConsumer(ctx, js, stream, desired); raced == nil {
		return consumer, nil
	}

	carryLaneAckFloor(&desired, info)
	return replacePullConsumer(js, stream, desired, err)
}

// provisionPullConsumer reads the durable's current shape, echoes the fields an
// update may not change, and issues it. The live info comes back alongside the
// error so the caller can carry the ack floor onto a replacement without a
// second read.
func provisionPullConsumer(
	ctx context.Context,
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
) (jsapi.Consumer, *jsapi.ConsumerInfo, error) {
	info, err := pullConsumerInfo(ctx, js, stream, desired.Name)
	if err != nil {
		return nil, nil, err
	}
	// The start position is immutable and belongs to whichever pod created the
	// shared durable. Echoing the server's own value keeps every later pod's
	// provision an update; sending DeliverNew at a consumer created elsewhere
	// would be rejected on every boot after the first.
	if info != nil {
		desired.DeliverPolicy = info.Config.DeliverPolicy
		desired.OptStartSeq = info.Config.OptStartSeq
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, desired)
	return consumer, info, err
}

// replacePullConsumer performs the delete + recreate an immutable-field
// transition requires, on its own budget so the create half cannot inherit an
// already-spent deadline from the update that failed — a delete that succeeds
// followed by a create that times out leaves the lane with no consumer at all.
// The caller has rewritten desired's delivery position, so the recreation
// resumes at the fleet's shared ack floor instead of replaying it.
func replacePullConsumer(
	js pullConsumerProvisioner,
	stream string,
	desired jsapi.ConsumerConfig,
	cause error,
) (jsapi.Consumer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), laneReplaceTimeout)
	defer cancel()

	if err := js.DeleteConsumer(ctx, stream, desired.Name); err != nil &&
		!errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, fmt.Errorf(
			"bus: provision pull consumer %q: %w (replace failed: %v)", desired.Name, cause, err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, desired)
	if err != nil {
		return nil, fmt.Errorf("bus: recreate pull consumer %q: %w", desired.Name, err)
	}
	return consumer, nil
}

func pullConsumerInfo(ctx context.Context, js pullConsumerProvisioner, stream, name string) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.Consumer(ctx, stream, name)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := consumer.Info(ctx)
	if errors.Is(err, jsapi.ErrConsumerNotFound) {
		return nil, nil
	}
	return info, err
}

// pullDelivery pairs a decoded message with the core message the shared helpers
// speak, so the retry path can re-send the original bytes and headers without
// re-encoding them.
type pullDelivery struct {
	wire *nats.Msg
	msg  *Message
}

// pullSubscriber is the shared-durable delivery adapter. One fetch loop per pod
// feeds one lane channel; every consumer unit in the process reads that channel,
// so the pod keeps exactly one binding however far the routine autoscaler grows,
// and the fleet keeps exactly one consumer however far the pod autoscaler grows.
//
// There is no receipt queue here, unlike the flow adapter. Nothing is being
// pushed: the loop asks for the next batch only once it has handed the previous
// one out, so a slow handler pool is backpressure onto the server's pending set
// rather than a queue this process has to size against a flow-control window.
type pullSubscriber struct {
	nc       *nats.Conn
	consumer jsapi.Consumer
	stream   string
	subject  string
	name     string
	log      *zap.Logger

	batch    int
	maxWait  time.Duration
	ackEvery time.Duration

	retried   atomic.Int64
	dropped   atomic.Int64
	fetchErrs atomic.Int64
	ackErrs   atomic.Int64
	closed    atomic.Bool

	// ackMu guards the newest delivery the floor has not yet covered. Both the
	// fetch loop and the ack timer publish the floor, and two acks racing would
	// otherwise be free to move it backwards.
	ackMu   sync.Mutex
	pending jsapi.Msg

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
	nc, err := nats.Connect(busURL(cfg.url), busOptions(cfg.group+"-pull")...)
	if err != nil {
		return nil, err
	}
	consumer, err := ensurePullConsumer(nc, cfg.stream, pullConsumerConfig(cfg.subject, name))
	if err != nil {
		nc.Close()
		return nil, err
	}
	s := newPullSubscriber(cfg, nc, consumer, name)
	s.start()
	return s, nil
}

func newPullSubscriber(cfg flowLaneConfig, nc *nats.Conn, consumer jsapi.Consumer, name string) *pullSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &pullSubscriber{
		nc: nc, consumer: consumer, stream: cfg.stream, subject: cfg.subject,
		name: name, log: log,
		batch: pullFetchBatch(), maxWait: pullFetchMaxWait(), ackEvery: pullAckEvery(),
		output:  make(chan *Message),
		closeCh: make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (s *pullSubscriber) start() {
	s.workers.Add(2)
	go s.pump()
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

func (s *pullSubscriber) pump() {
	defer s.workers.Done()
	for s.pumpIterator() {
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
func (s *pullSubscriber) pumpIterator() bool {
	iter, err := s.consumer.Messages(jsapi.PullMaxMessages(s.batch))
	if err != nil {
		return s.noteFetchError(err)
	}
	release := s.stopIteratorOnClose(iter)
	defer release()
	sinceFloor := 0
	for {
		wire, err := iter.Next()
		if err != nil {
			// Cover everything already handed out before deciding whether the
			// error is shutdown or a transport failure worth backing off on.
			s.advanceFloor()
			if s.closed.Load() {
				return false
			}
			return s.noteFetchError(err)
		}
		if !s.deliver(wire) {
			return false
		}
		if sinceFloor++; sinceFloor >= s.batch {
			s.advanceFloor()
			sinceFloor = 0
		}
	}
}

// stopIteratorOnClose unparks a blocked Next the moment the binding closes.
// The returned release tears the watcher down when the iterator ends first;
// stopping twice is safe.
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
		s.inflight.Done()
		return false
	}
}

func (s *pullSubscriber) decode(wire jsapi.Msg) (pullDelivery, bool) {
	core := pullWireMessage(wire)
	msg, err := messageFromNATS(core)
	if err != nil {
		s.log.Warn("dropping malformed lane delivery",
			zap.String("subject", s.subject), zap.Error(err))
		return pullDelivery{}, false
	}
	msg.SetContext(s.laneContext())
	return pullDelivery{wire: core, msg: msg}, true
}

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
	core := &nats.Msg{
		Subject: wire.Subject(),
		Reply:   wire.Reply(),
		Header:  wire.Headers(),
		Data:    wire.Data(),
	}
	metadata, err := wire.Metadata()
	if err != nil || metadata.Sequence.Stream == 0 {
		return core
	}
	if core.Header == nil {
		core.Header = nats.Header{}
	}
	if core.Header.Get(MessageIDHeader) == "" && core.Header.Get(legacyMessageIDHeader) == "" {
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
// Ordering inside a batch is the server's, so the last one recorded is the
// highest one received.
func (s *pullSubscriber) noteReceipt(wire jsapi.Msg) {
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
	if count := s.fetchErrs.Add(1); count == 1 || count%1_000 == 0 {
		s.log.Warn("lane fetch failed",
			zap.String("stream", s.stream),
			zap.String("subject", s.subject),
			zap.String("consumer", s.name),
			zap.Int64("fetch_errors", count),
			zap.Error(err))
	}
	return s.pause(s.maxWait)
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
	// The fetch loop is the only sender and has exited with the workers.
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
	if !drained {
		return errors.New("bus: timed out draining pull lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush pull lane acknowledgements: %w", flushErr)
	}
	return nil
}
