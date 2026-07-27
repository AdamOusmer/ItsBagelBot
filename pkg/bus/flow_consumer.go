package bus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/pkg/env"

	"github.com/bytedance/sonic"
	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

const (
	flowControlHeartbeat = time.Second
	flowMaxAckPending    = 20_000
	flowRetryTimeout     = 5 * time.Second
	flowResultWait       = 30 * time.Second

	// flowProvisionTimeout bounds an ordinary create/update. The replacement path
	// gets its own, much longer budget: a delete that succeeds followed by a
	// create that times out leaves the stream with no consumer at all.
	flowProvisionTimeout = 5 * time.Second
	flowReplaceTimeout   = 20 * time.Second

	// The server pins this ack policy's heartbeat to exactly one second, so three
	// missed beats is the earliest safe conclusion that the consumer behind this
	// delivery subject is gone. A push consumer learns about deletion only from
	// heartbeat loss: no 404 and no 409 is ever published to a push delivery
	// subject in 2.14.3.
	flowHeartbeatGrace = 3 * time.Second
	flowWatchdogTick   = time.Second

	// flowInactiveThreshold garbage-collects this pod's own consumer once the pod
	// is gone. nats-server 2.14.3 arms a delete timer whenever a push consumer's
	// delivery subject loses interest and honours the threshold for durables as
	// well as ephemerals (consumer.go updateInactiveThreshold / deleteNotActive),
	// so a consumer named after a pod that never comes back deletes itself. It is
	// deliberately far longer than a reconnect: a pod that reattaches inside the
	// window keeps its consumer and its ack floor.
	flowInactiveThreshold = 5 * time.Minute

	// flowSessionResetGap is the backwards jump in consumer sequence that can only
	// be a new server-side session. A delete + recreate restarts the consumer
	// sequence at 1 while the stream sequence keeps climbing, so anything further
	// back than a whole ack window is a reset rather than reordering.
	flowSessionResetGap = flowMaxAckPending
)

// Push status protocol (nats-server 2.14 consumer.go, nats.go jetstream/push.go).
// A status delivery carries no payload; the fleet answers two of them:
//
//	100 "FlowControl Request"  arrives inline in the delivery stream, reply set
//	                           to the server's current flow-control id.
//	100 "Idle Heartbeat"       carries Nats-Consumer-Stalled only while a
//	                           flow-control response is still outstanding.
//
// Under AckFlowControl the response headers ARE the acknowledgement: the server
// reads Nats-Last-Consumer/Nats-Last-Stream off the response and advances the
// replicated ack floor to that stream sequence in one operation.
const (
	statusHeader          = "Status"
	descriptionHeader     = "Description"
	lastConsumerHeader    = "Nats-Last-Consumer"
	lastStreamHeader      = "Nats-Last-Stream"
	consumerStalledHeader = "Nats-Consumer-Stalled"

	controlStatus = "100"
)

// RetryCountHeader bounds the flow path's redelivery to a single hop. It is an
// ordinary application header, which is exactly why it can carry the budget: the
// message scheduler strips every Nats-* header from what it emits and carries
// application headers through verbatim, so this count survives the hop while the
// schedule's own control headers do not.
const RetryCountHeader = "Bagelbot-Retry"

const (
	// retryLanePrefix is the fleet's retry namespace, captured by
	// TWITCH_INGRESS_RETRY. Both the schedule row and the message it emits live
	// under it, because nats-server requires a schedule's Nats-Schedule-Target to
	// collide with a subject of the very stream that stores the schedule
	// (stream.go, JSMessageSchedulesTargetInvalid): a schedule can never deliver
	// into a different stream, so the retry hop cannot land back on the ingress
	// lane itself.
	retryLanePrefix = "twitch.ingress.retry."

	// retryScheduleTTL expires the emitted retry, not the schedule row. A retry
	// nobody consumed within this window is a stale chat event and must not be
	// acted on late; the one-shot row purges itself as it fires.
	retryScheduleTTL = "30s"

	defaultFlowRetryDelay = 3 * time.Second
)

// isHotIngressLane limits receipt-level acknowledgement flow control to the
// perishable high-rate event lanes. Stream/status control messages retain the
// ordinary explicit-ACK contract because losing one while an application
// process exits has a much larger blast radius than replaying a chat event.
// Work-queue streams are excluded by the same rule: their retention deletes on
// ack and therefore requires per-message explicit acknowledgement.
func isHotIngressLane(stream, subject string) bool {
	if stream != TwitchIngressStream.Name {
		return false
	}
	return strings.HasSuffix(subject, ".premium") || strings.HasSuffix(subject, ".standard")
}

// FlowConsumeEnabled reports whether the hot ingress lanes bind receipt-level
// flow-controlled consumers. NATS_CONSUME_FLOW=off returns every lane to the
// explicit-ACK subscriber without a redeploy of the bus.
func FlowConsumeEnabled() bool {
	return env.Get("NATS_CONSUME_FLOW", "on") != "off"
}

// RetryLaneSubject is the retry lane a failed hot-lane event is scheduled onto.
// It is the schedule's target subject, and the subject a consumer binds to pick
// the retry back up.
func RetryLaneSubject(lane string) string {
	return retryLanePrefix + subjectLeaf(lane)
}

func subjectLeaf(subject string) string {
	if cut := strings.LastIndex(subject, "."); cut >= 0 {
		return subject[cut+1:]
	}
	return subject
}

// flowConsumerConfig keeps the hot consumer R3 while removing per-message
// consumer consensus. NATS AckFlowControl does not replicate delivery state
// before each push; a flow response advances the replicated ACK floor for a
// whole window.
//
// This is deliberately receipt-level acknowledgement. A hard process/node loss
// after receipt but before handler completion can lose as many as
// flowMaxAckPending (20,000) deliveries for this consumer. Graceful shutdown
// drains them; replay-sensitive control lanes remain on explicit ACK instead.
//
// There is deliberately NO DeliverGroup. The server publishes both the
// flow-control request and the idle heartbeat to the delivery subject as
// ordinary account messages with no queue-group targeting, so exactly one
// arbitrary member would receive each and would answer for the whole group out
// of its own cursor — and the ack is cumulative, so that one member's answer
// would acknowledge work still in flight on every other member. The floor would
// have to be the minimum across members, which the protocol cannot express.
// Each pod therefore owns its own consumer, its own delivery subject and its own
// cursor, and consequently receives the whole lane rather than a share of it:
// handlers must be idempotent and must not assume the fleet splits a lane.
//
// DeliverNew applies only to a first-ever creation. The lane stream retains five
// minutes of firehose, so a consumer created at DeliverAll would open by
// replaying every retained chat event; ensureFlowConsumer preserves the
// creation-time position on update and resumes at the predecessor's ack floor on
// replacement, falling back to DeliverNew rather than to an unknown inherited
// policy when that floor is unavailable.
func flowConsumerConfig(subject, name string) jsapi.ConsumerConfig {
	return jsapi.ConsumerConfig{
		Name:           name,
		Durable:        name,
		Description:    "ItsBagelBot R3 flow-controlled ingress lane consumer",
		DeliverPolicy:  jsapi.DeliverNewPolicy,
		AckPolicy:      jsapi.AckFlowControlPolicy,
		MaxDeliver:     -1,
		FilterSubject:  subject,
		ReplayPolicy:   jsapi.ReplayInstantPolicy,
		MaxAckPending:  flowMaxAckPending,
		DeliverSubject: "_INBOX.BAGEL." + subjectToken(name),
		FlowControl:    true,
		// The server rejects any other heartbeat for this ack policy: the same
		// interval bounds the stalled-source timeout it shares with sourcing.
		IdleHeartbeat: flowControlHeartbeat,
		// One consumer per pod means one consumer per pod that dies. The server
		// deletes it once its delivery subject has had no interest for this long.
		InactiveThreshold: flowInactiveThreshold,
		// Inherit the parent stream's replica count. TWITCH_INGRESS is R3 in
		// production, so its consumer state remains R3 without a second replica
		// setting that can drift from the stream during reconciliation.
		Replicas:      0,
		MemoryStorage: true,
		Metadata:      map[string]string{managedConsumerMetadata: "true"},
	}
}

// flowConsumerName is this pod's own durable. The group and subject keep the
// fleet-wide naming contract; the pod identity is what makes the consumer
// single-subscriber, which is the only shape AckFlowControl has coherent
// acknowledgement semantics for.
func flowConsumerName(group, subject string) string {
	return durableName(group, subject) + "_" + podIdentity()
}

// podIdentity resolves a token that is stable for the life of a pod and distinct
// between pods. A restarted pod under the same name deliberately reuses its
// consumer, which keeps the ack floor; anything else falls back to a random
// token that the inactive threshold cleans up.
func podIdentity() string {
	name := env.Get("POD_NAME", env.Get("HOSTNAME", ""))
	if name == "" {
		name, _ = os.Hostname()
	}
	if name == "" {
		name = nuid.Next()
	}
	return consumerToken(name)
}

// consumerToken reduces an arbitrary identity to the characters a JetStream
// consumer name accepts, and bounds its length so the durable stays inside the
// server's 255-character name limit.
func consumerToken(name string) string {
	token := strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
			return char
		case char >= '0' && char <= '9', char == '-', char == '_':
			return char
		default:
			return '_'
		}
	}, name)
	if len(token) > 48 {
		token = token[:48]
	}
	return token
}

// recoveryFlowConsumerConfig rebuilds a consumer the watchdog found gone.
// Recovery resumes just past this process's receipt cursor rather than at the
// server's last-sent position, so nothing that was pushed but never arrived is
// skipped. An empty cursor keeps the creation default rather than guessing.
func recoveryFlowConsumerConfig(subject, name string, position flowPosition) jsapi.ConsumerConfig {
	desired := flowConsumerConfig(subject, name)
	if position.stream == 0 {
		return desired
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = position.stream + 1
	return desired
}

// immutableConsumerFieldErrors are the only 2.14 update rejections that justify
// destroying a live consumer. Everything else — a context deadline against a busy
// meta layer, no responders during an election, a lost race with another replica
// — must propagate unchanged: deleting on those turns a transient API failure
// into a lane-wide delivery reset for every pod bound to the stream.
var immutableConsumerFieldErrors = []string{
	"ack policy can not be updated",
	"flow control can not be updated",
	"heart beats can not be updated",
}

func requiresConsumerReplacement(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, immutable := range immutableConsumerFieldErrors {
		if strings.Contains(message, immutable) {
			return true
		}
	}
	return false
}

// ensureFlowConsumer provisions this pod's flow consumer and returns the
// delivery subject to bind, which is the server's value whenever one exists.
func ensureFlowConsumer(nc *nats.Conn, stream string, desired jsapi.ConsumerConfig) (string, error) {
	js, err := jsapi.NewWithDomain(nc, JSDomain())
	if err != nil {
		return "", fmt.Errorf("bus: modern jetstream context: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), flowProvisionTimeout)
	defer cancel()

	info, err := flowConsumerInfo(ctx, js, stream, desired.Name)
	if err != nil {
		return "", err
	}
	if info == nil {
		_, err = js.CreateOrUpdatePushConsumer(ctx, stream, desired)
		return desired.DeliverSubject, err
	}

	// Preserve the stable binding and the creation-time start position on an
	// ordinary update; both are immutable, so sending anything else guarantees a
	// rejection every boot.
	desired.DeliverSubject = info.Config.DeliverSubject
	desired.DeliverPolicy = info.Config.DeliverPolicy
	desired.OptStartSeq = info.Config.OptStartSeq
	if _, err = js.UpdatePushConsumer(ctx, stream, desired); err == nil {
		return desired.DeliverSubject, nil
	}
	if !requiresConsumerReplacement(err) {
		return "", fmt.Errorf("bus: update flow consumer %q: %w", desired.Name, err)
	}

	carryFlowAckFloor(&desired, info)
	return desired.DeliverSubject, replaceFlowConsumer(js, stream, desired, err)
}

func flowConsumerInfo(ctx context.Context, js jsapi.JetStream, stream, name string) (*jsapi.ConsumerInfo, error) {
	consumer, err := js.PushConsumer(ctx, stream, name)
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

// replaceFlowConsumer performs the delete + recreate an immutable-field
// transition requires, on its own budget so the create half cannot inherit an
// already-spent deadline from the update that failed. The caller has rewritten
// desired's delivery position, so the recreation never replays handled messages.
func replaceFlowConsumer(
	js jsapi.JetStream,
	stream string,
	desired jsapi.ConsumerConfig,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), flowReplaceTimeout)
	defer cancel()

	if err := js.DeleteConsumer(ctx, stream, desired.Name); err != nil &&
		!errors.Is(err, jsapi.ErrConsumerNotFound) {
		return fmt.Errorf("bus: update flow consumer %q: %w (replace failed: %v)", desired.Name, cause, err)
	}
	if _, err := js.CreateOrUpdatePushConsumer(ctx, stream, desired); err != nil {
		return fmt.Errorf("bus: recreate flow consumer %q: %w", desired.Name, err)
	}
	return nil
}

// carryFlowAckFloor pins the replacement's start position. An unknown ack floor
// must never fall through to the predecessor's own DeliverPolicy: the
// explicit-ACK lane consumer this replaces is DeliverAll, so inheriting it opens
// the replacement on the whole retained firehose and re-executes every chat
// command in the window. DeliverNew loses the unacked tail instead, which is the
// bounded failure of the two.
func carryFlowAckFloor(desired *jsapi.ConsumerConfig, info *jsapi.ConsumerInfo) {
	if info == nil || info.AckFloor.Stream == 0 {
		desired.DeliverPolicy = jsapi.DeliverNewPolicy
		desired.OptStartSeq = 0
		return
	}
	desired.DeliverPolicy = jsapi.DeliverByStartSequencePolicy
	desired.OptStartSeq = info.AckFloor.Stream + 1
}

// flowPosition is one immutable receipt cursor sample.
type flowPosition struct {
	consumer uint64
	stream   uint64
}

// flowCursor tracks the highest delivery this process has actually taken off
// the wire. The flow response never claims more than this, so the replicated
// ack floor can never move past a message the pod did not receive.
type flowCursor struct {
	mu       sync.Mutex
	position flowPosition
}

// record gates on the STREAM sequence, which is monotonic across a consumer
// delete + recreate. The consumer sequence is not: the server restarts it at 1
// whatever start sequence the recreated consumer opens at, so a cursor gated on
// it rejects every delivery from the new session forever and freezes the ack
// floor at MaxAckPending with no redelivery and no error. It returns false for a
// sample that did not advance the cursor.
func (c *flowCursor) record(consumer, stream uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isSessionReset(consumer) {
		c.position = flowPosition{consumer: consumer, stream: stream}
		return true
	}
	if stream <= c.position.stream {
		return false
	}
	c.position = flowPosition{consumer: consumer, stream: stream}
	return true
}

// isSessionReset recognises the server's own reset signature: a consumer
// sequence further back than a whole ack window cannot be reordering.
func (c *flowCursor) isSessionReset(consumer uint64) bool {
	return c.position.consumer > consumer &&
		c.position.consumer-consumer > flowSessionResetGap
}

func (c *flowCursor) snapshot() flowPosition {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.position
}

// reset drops the cursor when this process recreates its own consumer. The new
// consumer's sequences start over, so keeping the old pair would answer flow
// control with an ack floor from a session the server no longer has.
func (c *flowCursor) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.position = flowPosition{}
}

// flowControlResponse builds the receipt-level acknowledgement for one push
// status message, or nil when the status needs no answer (a heartbeat sent
// while no flow-control window is outstanding).
//
// A response is mandatory even with nothing received: the server reopens the
// delivery window purely on the response arriving, and only reads an ack floor
// out of the headers when they are present.
func flowControlResponse(control *nats.Msg, position flowPosition) *nats.Msg {
	reply := control.Reply
	if stalled := control.Header.Get(consumerStalledHeader); stalled != "" {
		reply = stalled
	}
	if reply == "" {
		return nil
	}
	response := nats.NewMsg(reply)
	if position.stream == 0 {
		return response
	}
	response.Header.Set(lastConsumerHeader, strconv.FormatUint(position.consumer, 10))
	response.Header.Set(lastStreamHeader, strconv.FormatUint(position.stream, 10))
	return response
}

type flowLaneConfig struct {
	url     string
	stream  string
	subject string
	group   string
	log     *zap.Logger
}

// flowDelivery pairs a decoded message with the wire it arrived on, so the
// retry path can re-send the original bytes and headers without re-encoding.
type flowDelivery struct {
	wire *nats.Msg
	msg  *Message
}

// flowSubscriber is the receipt-level delivery adapter for the hot ingress
// lanes. It binds this pod's own AckFlowControl push consumer through a plain
// core subscription on the consumer's delivery subject: individual messages
// carry no acknowledgement traffic at all, and the flow-control responses this
// adapter sends advance the whole window's ack floor in one replicated step.
//
// The subscription is deliberately a core one rather than nats.go's own push
// binding. Both of nats.go's flow-control handlers answer a request with an
// empty message, and the server only reads an ack floor out of a response that
// carries the sequence headers — so under AckFlowControl their responses reopen
// the delivery window without ever acknowledging anything, and the consumer's
// pending set would grow until MaxAckPending stalled it.
//
// One subscriber serves every consumer unit in the pod: the units share the one
// output channel, so the pod keeps exactly one consumer, one cursor and one
// flow-control conversation however far the routine autoscaler grows.
type flowSubscriber struct {
	nc             *nats.Conn
	stream         string
	subject        string
	group          string
	consumer       string
	deliverSubject string
	log            *zap.Logger

	cursor      flowCursor
	dropped     atomic.Int64
	retried     atomic.Int64
	stale       atomic.Int64
	overrun     atomic.Int64
	lastControl atomic.Int64
	recovering  atomic.Bool
	closed      atomic.Bool

	sub     *nats.Subscription
	queue   chan flowDelivery
	output  chan *Message
	closeCh chan struct{}
	// ctx is the lane's own delivery context, carried on every message. Units come
	// and go around the shared binding, so a single unit's context cannot own the
	// cancellation every handler reads; this one is cancelled when the binding
	// itself is released.
	ctx    context.Context
	cancel context.CancelFunc

	workers sync.WaitGroup
	pending sync.WaitGroup
	once    sync.Once
}

// newFlowLaneSubscriber connects, provisions this pod's flow consumer, and binds
// the delivery subject the server reports.
func newFlowLaneSubscriber(cfg flowLaneConfig) (*flowSubscriber, error) {
	consumer := flowConsumerName(cfg.group, cfg.subject)

	// The connection name separates the two acknowledgement contracts in NATS
	// monitoring, so a mixed rollout is legible from connz alone.
	nc, err := nats.Connect(busURL(cfg.url), busOptions(cfg.group+"-flow")...)
	if err != nil {
		return nil, err
	}
	deliver, err := ensureFlowConsumer(nc, cfg.stream, flowConsumerConfig(cfg.subject, consumer))
	if err != nil {
		nc.Close()
		return nil, err
	}

	s := newFlowSubscriber(cfg, nc, consumer, deliver)
	if err := s.start(); err != nil {
		nc.Close()
		return nil, err
	}
	return s, nil
}

func newFlowSubscriber(cfg flowLaneConfig, nc *nats.Conn, consumer, deliver string) *flowSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &flowSubscriber{
		nc: nc, stream: cfg.stream, subject: cfg.subject, group: cfg.group,
		consumer: consumer, deliverSubject: deliver, log: log,
		// The queue is sized at the server's own message brake: the consumer stops
		// delivering at MaxAckPending, so a queue that deep cannot overflow before
		// the server has already stopped pushing.
		queue:   make(chan flowDelivery, flowMaxAckPending),
		output:  make(chan *Message),
		closeCh: make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (s *flowSubscriber) start() error {
	sub, err := s.nc.Subscribe(s.deliverSubject, s.deliveryCallback)
	if err != nil {
		return err
	}
	s.sub = sub
	s.lastControl.Store(time.Now().UnixNano())
	s.workers.Add(2)
	go s.pump()
	go s.watchHeartbeats()
	return nil
}

// Subscribe hands back the pod's single lane channel. Every consumer unit reads
// the same channel, which is what keeps one consumer per pod while the unit
// autoscaler grows and shrinks.
func (s *flowSubscriber) Subscribe(_ context.Context, subject string) (<-chan *Message, error) {
	if subject != s.subject {
		return nil, fmt.Errorf("bus: flow subscriber is bound to %q, not %q", s.subject, subject)
	}
	if s.closed.Load() {
		return nil, errors.New("bus: subscriber is closed")
	}
	return s.output, nil
}

// deliveryCallback runs on nats.go's single per-subscription goroutine. Status
// messages are answered here, before anything that can block: a flow-control
// request queued behind a data message the handler pool has not accepted yet
// leaves the server's window closed, and delivery stops for the whole consumer
// until the pool drains. Data is handed to a bounded queue and never waits.
func (s *flowSubscriber) deliveryCallback(wire *nats.Msg) {
	if status := wire.Header.Get(statusHeader); status != "" {
		s.handleStatus(wire, status)
		return
	}
	s.enqueue(wire)
}

// enqueue records the receipt cursor before queueing, so a flow-control request
// arriving straight after the delivery can already acknowledge it.
func (s *flowSubscriber) enqueue(wire *nats.Msg) {
	s.recordReceipt(wire)
	msg, err := messageFromNATS(wire)
	if err != nil {
		s.log.Warn("dropping malformed lane delivery", zap.String("subject", s.subject), zap.Error(err))
		return
	}
	msg.SetContext(s.laneContext())
	select {
	case s.queue <- flowDelivery{wire: wire, msg: msg}:
	default:
		// Withholding the flow-control response would stall the consumer; dropping
		// one delivery costs one event. The server's own MaxAckPending brake should
		// make this unreachable, so it is an alert, not a mode of operation.
		s.log.Warn("lane receipt queue overflowed",
			zap.String("subject", s.subject),
			zap.Int64("overrun_total", s.overrun.Add(1)))
	}
}

// laneContext keeps a directly-constructed subscriber (tests) usable without a
// context of its own.
func (s *flowSubscriber) laneContext() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *flowSubscriber) recordReceipt(wire *nats.Msg) {
	metadata, err := wire.Metadata()
	if err != nil {
		return
	}
	if s.cursor.record(metadata.Sequence.Consumer, metadata.Sequence.Stream) {
		return
	}
	// Deliveries are arriving that the cursor refuses to account for, so the ack
	// floor has stopped advancing while the pending set keeps growing.
	if stale := s.stale.Add(1); stale == 1 || stale%10_000 == 0 {
		s.log.Warn("receipt cursor is not advancing under delivery",
			zap.String("subject", s.subject),
			zap.Uint64("delivery_stream_seq", metadata.Sequence.Stream),
			zap.Int64("stale_total", stale))
	}
}

// pump moves queued deliveries onto the shared lane channel. It is the only
// sender, so the channel can be closed once it has exited.
func (s *flowSubscriber) pump() {
	defer s.workers.Done()
	for {
		select {
		case delivery := <-s.queue:
			if !s.deliver(delivery) {
				return
			}
		case <-s.closeCh:
			return
		}
	}
}

func (s *flowSubscriber) deliver(delivery flowDelivery) bool {
	select {
	case s.output <- delivery.msg:
		s.pending.Add(1)
		go s.awaitResult(delivery)
		return true
	case <-s.closeCh:
		return false
	}
}

// awaitResult reconciles the handler's verdict. An ACK is receipt-level and
// needs no traffic at all: the flow-control responses already own the ack
// floor. Only a NACK costs anything, and it costs one scheduled retry.
func (s *flowSubscriber) awaitResult(delivery flowDelivery) {
	defer s.pending.Done()
	timer := time.NewTimer(flowResultWait)
	defer timer.Stop()

	select {
	case <-delivery.msg.Acked():
	case <-delivery.msg.Nacked():
		s.scheduleRetry(delivery)
	case <-timer.C:
	case <-s.closeCh:
	}
}

// scheduleRetry is the flow path's redelivery. The server holds no per-message
// pending state to NAK against — it does not even subscribe to $JS.ACK for this
// ack policy — so the adapter hands the event to the broker's own scheduler
// instead: a one-shot @at row on the retry stream that re-emits the payload onto
// the retry lane once the delay has passed, then purges itself.
func (s *flowSubscriber) scheduleRetry(delivery flowDelivery) {
	msg := delivery.msg
	if attempt := msg.Metadata.Get(RetryCountHeader); attempt != "" {
		s.dropRetry(msg, fmt.Errorf("one-hop retry budget exhausted at attempt %s", attempt))
		return
	}
	schedule := retryScheduleMsg(s.subject, delivery.wire, flowRetryDelay(), time.Now())
	ack, err := s.nc.RequestMsg(schedule, flowRetryTimeout)
	if err != nil {
		s.dropRetry(msg, err)
		return
	}
	if err := pubAckError(ack); err != nil {
		s.dropRetry(msg, err)
		return
	}
	s.retried.Add(1)
}

// retryScheduleMsg builds the one-shot schedule row. The row's own subject is
// unique per retry because publishing a schedule rolls its subject up: a second
// retry on a shared subject would purge the first one before it ever fired.
//
// No Nats-Schedule-Time-Zone header is set. The server rejects the @at form
// outright when a time zone is present, even "UTC".
func retryScheduleMsg(lane string, wire *nats.Msg, delay time.Duration, now time.Time) *nats.Msg {
	target := RetryLaneSubject(lane)
	schedule := nats.NewMsg(target + "." + nuid.Next())
	schedule.Data = append([]byte(nil), wire.Data...)
	copyApplicationHeaders(schedule.Header, wire.Header)

	// Carry a stable idempotency key across the hop. An ingress-origin event
	// reaches the lane with no Bagelbot-Message-Id, so copyApplicationHeaders had
	// nothing to carry and the re-emitted retry would land under a fresh stream
	// sequence — a different Message.UUID that a consumer's dedup guard could not
	// recognise as the same event. Stamp the delivery's already-resolved identity
	// (the JetStream stream-seq fallback for those events) so Message.UUID stays
	// stable across the retry. A Go-origin header copied above is left untouched.
	if wire.Header.Get(MessageIDHeader) == "" {
		schedule.Header.Set(MessageIDHeader, messageIdentity(wire))
	}

	schedule.Header.Set(jsapi.ScheduleHeader, "@at "+now.Add(delay).UTC().Format(time.RFC3339))
	schedule.Header.Set(jsapi.ScheduleTargetHeader, target)
	schedule.Header.Set(jsapi.ScheduleTTLHeader, retryScheduleTTL)
	schedule.Header.Set(RetryCountHeader, "1")
	return schedule
}

// copyApplicationHeaders carries the fleet's own headers across the hop and
// leaves every Nats-* header behind. The scheduler strips those from what it
// emits anyway, and the expectation headers among them would be re-evaluated
// against the retry stream and reject the publish.
func copyApplicationHeaders(into, from nats.Header) {
	for key, values := range from {
		if len(values) == 0 || strings.HasPrefix(key, "Nats-") {
			continue
		}
		into.Set(key, values[0])
	}
}

// flowRetryDelay is how long a failed event waits before the broker re-emits it.
// It exists to let a transient downstream failure clear, so it is tunable
// without a rebuild.
func flowRetryDelay() time.Duration {
	delay := env.GetDuration("NATS_FLOW_RETRY_DELAY", defaultFlowRetryDelay)
	if delay <= 0 {
		return defaultFlowRetryDelay
	}
	return delay
}

// pubAckError reports a JetStream publish rejection. A schedule the broker
// refuses (schedules disabled, an invalid target, a denied subject) answers with
// a normal PubAck carrying an error, which is silent unless it is read.
func pubAckError(ack *nats.Msg) error {
	if ack == nil {
		return errors.New("bus: retry schedule got no publish acknowledgement")
	}
	var response struct {
		Error *struct {
			ErrCode     int    `json:"err_code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := sonic.Unmarshal(ack.Data, &response); err != nil {
		return fmt.Errorf("bus: unreadable retry publish acknowledgement: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("bus: retry schedule rejected (%d): %s",
			response.Error.ErrCode, response.Error.Description)
	}
	return nil
}

func (s *flowSubscriber) dropRetry(msg *Message, cause error) {
	s.log.Warn("dropping failed lane event",
		zap.String("subject", s.subject),
		zap.String("message_id", msg.UUID),
		zap.Int64("dropped_total", s.dropped.Add(1)),
		zap.Error(cause))
}

// handleStatus answers control messages and records that the consumer behind
// this delivery subject is alive. Nothing but 100-class control messages is ever
// published to a push delivery subject in 2.14.3 — every 404 and 409 goes to a
// pull request's reply — so a non-100 status is logged rather than treated as a
// deletion signal. Deletion is detected by the heartbeat watchdog instead.
func (s *flowSubscriber) handleStatus(control *nats.Msg, status string) {
	if status == controlStatus {
		s.lastControl.Store(time.Now().UnixNano())
		s.noteDeliveryGap(control)
		s.respondFlowControl(control)
		return
	}
	s.log.Warn("unexpected flow consumer status",
		zap.String("subject", s.subject),
		zap.String("status", status),
		zap.String("description", control.Header.Get(descriptionHeader)))
}

// noteDeliveryGap compares what the heartbeat says the server has sent with what
// this process has taken off the wire. A gap wider than the whole ack window
// cannot be traffic in flight: after a leader change the server resumes past its
// last delivered sequence and never redelivers what fell in between, and this ack
// policy has no other redelivery. It is reported rather than repaired —
// $JS.API.CONSUMER.RESET would rewind to the ack floor and replay everything
// already handled since it.
func (s *flowSubscriber) noteDeliveryGap(control *nats.Msg) {
	sent, err := strconv.ParseUint(control.Header.Get(lastConsumerHeader), 10, 64)
	if err != nil {
		return
	}
	received := s.cursor.snapshot().consumer
	if !deliveriesWereLost(sent, received) {
		return
	}
	s.log.Warn("flow consumer deliveries were never received",
		zap.String("subject", s.subject),
		zap.Uint64("server_last_consumer", sent),
		zap.Uint64("received_consumer", received))
}

// deliveriesWereLost separates a legitimate in-flight window from a real gap.
// The server never has more than MaxAckPending deliveries outstanding, so
// anything beyond that is traffic that will never arrive.
func deliveriesWereLost(sent, received uint64) bool {
	return sent > received+flowMaxAckPending
}

func (s *flowSubscriber) respondFlowControl(control *nats.Msg) {
	response := flowControlResponse(control, s.cursor.snapshot())
	if response == nil {
		return
	}
	if err := s.nc.PublishMsg(response); err != nil {
		// The server reopens the window on its next heartbeat's stalled marker,
		// so a lost response costs latency rather than the binding.
		s.log.Warn("flow-control response failed", zap.String("subject", s.subject), zap.Error(err))
	}
}

// watchHeartbeats is the only way this binding learns its consumer is gone. The
// server pins the heartbeat at one second and publishes no deletion status to a
// push subject, so silence on the delivery subject is the signal: the lane would
// otherwise sit at zero events per second, healthy and Ready, indefinitely.
func (s *flowSubscriber) watchHeartbeats() {
	defer s.workers.Done()
	ticker := time.NewTicker(flowWatchdogTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return
		case now := <-ticker.C:
			if heartbeatLost(now, s.lastControl.Load()) {
				s.recoverConsumer()
			}
		}
	}
}

// heartbeatLost is the watchdog's decision. The heartbeat is pinned at one
// second for this ack policy, so three missed beats is silence, not jitter.
func heartbeatLost(now time.Time, lastControlUnixNano int64) bool {
	return now.Sub(time.Unix(0, lastControlUnixNano)) > flowHeartbeatGrace
}

// recoverConsumer re-provisions a consumer the watchdog found silent. The
// delivery subject is deterministic, so the existing subscription keeps
// receiving once the consumer is back; single-flighting stops the watchdog from
// racing itself while a slow provision is in progress.
func (s *flowSubscriber) recoverConsumer() {
	if !s.recovering.CompareAndSwap(false, true) {
		return
	}
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()
		defer s.recovering.Store(false)
		s.reprovision()
	}()
}

func (s *flowSubscriber) reprovision() {
	desired := recoveryFlowConsumerConfig(s.subject, s.consumer, s.cursor.snapshot())
	if _, err := ensureFlowConsumer(s.nc, s.stream, desired); err != nil {
		s.log.Error("flow consumer recovery failed", zap.String("subject", s.subject), zap.Error(err))
		return
	}
	// The recreated consumer restarts its own sequences, so the old pair would
	// answer flow control for a session the server no longer has.
	s.cursor.reset()
	s.lastControl.Store(time.Now().UnixNano())
	s.log.Warn("flow consumer re-provisioned after heartbeat loss",
		zap.String("subject", s.subject), zap.String("consumer", s.consumer))
}

func (s *flowSubscriber) Close() error {
	var err error
	s.once.Do(func() { err = s.shutdown() })
	return err
}

func (s *flowSubscriber) shutdown() error {
	s.closed.Store(true)
	if s.cancel != nil {
		s.cancel()
	}
	if s.sub != nil {
		if unsub := s.sub.Unsubscribe(); unsub != nil && !errors.Is(unsub, nats.ErrBadSubscription) {
			s.log.Warn("flow subscription stop failed", zap.String("subject", s.subject), zap.Error(unsub))
		}
	}
	close(s.closeCh)
	s.workers.Wait()

	deadline := time.NewTimer(subscriberDrainTimeout)
	defer deadline.Stop()
	drained := waitGroupBefore(&s.pending, deadline.C)

	err := s.flushAndClose(drained)
	close(s.output)
	return err
}

// flushAndClose pushes the last flow-control response and retry schedule onto
// the wire before the connection goes away. Closing without a flush discards
// whatever is still buffered, which on this lane is exactly the acknowledgement
// that would have released the final window.
func (s *flowSubscriber) flushAndClose(drained bool) error {
	flushErr := s.nc.FlushTimeout(2 * time.Second)
	s.nc.Close()
	if !drained {
		return errors.New("bus: timed out draining flow lane deliveries")
	}
	if flushErr != nil {
		return fmt.Errorf("bus: flush flow lane acknowledgements: %w", flushErr)
	}
	return nil
}
