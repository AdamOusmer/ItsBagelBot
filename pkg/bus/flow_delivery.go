package bus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	// flowControlPendingBytes is the server's flow-control window for this ack
	// policy: nats-server 2.14.3 holds JsFlowControlMaxPending (32 MiB) of
	// delivered payload outstanding before it shuts the window until a response
	// arrives.
	flowControlPendingBytes = 32 << 20

	// flowWireBytesFloor is the smallest lane delivery the queue is sized for. A
	// live R3 run measured ~865 B on the wire per ingress event; sizing at 800
	// deliberately rounds the derived depth UP, because the failure this guards
	// against is a queue too shallow for the window, never one too deep.
	flowWireBytesFloor = 800

	// flowQueueDepth is how many deliveries the callback may hold. It is derived
	// from the BYTE window and not from flowMaxAckPending, because the message
	// brake is not what binds here: under AckFlowControl the server keeps no
	// per-message pending set to stop at, it stops on the byte window, and 32 MiB
	// of ~865 B deliveries ramps to roughly 38k messages in flight — nearly twice
	// the 20k the message figure suggests. A queue cut to flowMaxAckPending
	// therefore overflows during perfectly healthy delivery: one repro dropped
	// 26k deliveries in three seconds that way, silently, because a drop here
	// costs an event rather than the binding. The depth has to sit above the
	// ramped window, so it is derived at the wire floor with the margin that
	// implies.
	flowQueueDepth = flowControlPendingBytes / flowWireBytesFloor
)

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
	stallStreak atomic.Int64
	wedged      atomic.Int64
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

// binding is what this subscriber re-provisions against. The three fields are
// fixed for the subscriber's life, so the triple is derived rather than stored
// twice — a second copy is a second thing to keep in step with the consumer the
// server actually holds.
func (s *flowSubscriber) binding() laneBinding {
	return laneBinding{stream: s.stream, subject: s.subject, consumer: s.consumer}
}

// newFlowLaneSubscriber connects, provisions this pod's flow consumer, and binds
// the delivery subject the server reports.
func newFlowLaneSubscriber(cfg flowLaneConfig) (*flowSubscriber, error) {
	lane := laneBinding{
		stream:   cfg.stream,
		subject:  cfg.subject,
		consumer: flowConsumerName(cfg.group, cfg.subject),
	}

	// The connection name separates the two acknowledgement contracts in NATS
	// monitoring, so a mixed rollout is legible from connz alone.
	nc, err := nats.Connect(busURL(endpoint(cfg.url)), busOptions(clientName(cfg.group)+"-flow")...)
	if err != nil {
		return nil, err
	}
	deliver, err := ensureFlowConsumer(nc, lane, flowConsumerConfig(lane))
	if err != nil {
		nc.Close()
		return nil, err
	}

	s := newFlowSubscriber(cfg, nc, lane, deliver)
	if err := s.start(); err != nil {
		nc.Close()
		return nil, err
	}
	return s, nil
}

// There is deliberately no exported constructor that takes a stream and subject
// directly. One existed for the load rig, and it was the only way past the two
// guards the service path applies: streamForTopic, which refuses a subject the
// catalog does not own, and isHotIngressLane, which refuses any stream but the
// two ingress lane streams. With the rig gone, every caller goes through NewSubscriber and
// those two keep deciding which lanes get receipt-level acknowledgement.

func newFlowSubscriber(cfg flowLaneConfig, nc *nats.Conn, lane laneBinding, deliver string) *flowSubscriber {
	log := cfg.log
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &flowSubscriber{
		nc: nc, stream: lane.stream, subject: lane.subject, group: cfg.group,
		consumer: lane.consumer, deliverSubject: deliver, log: log,
		// Sized at the server's own byte window, not at MaxAckPending: the window
		// is what actually stops delivery under this ack policy, and it ramps well
		// past the message figure.
		queue:   make(chan flowDelivery, flowQueueDepth),
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
	// A delivery is the proof the window reopened, so it ends any wedge streak the
	// stalled heartbeats had accumulated.
	s.stallStreak.Store(0)
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
		// one delivery costs one event. The queue is cut to sit above the server's
		// ramped flow-control window, so this stays an alert rather than a mode of
		// operation — a sustained count here means the window grew past what
		// flowQueueDepth was derived for and the derivation needs re-measuring.
		//
		// Throttled like the stale counter, and for the same reason: sustained
		// overflow is exactly the case where a line per drop would emit at the
		// event rate of the lane. The counter still moves on every drop, so the
		// magnitude is never lost — only the repetition is.
		if overrun := s.overrun.Add(1); overrun == 1 || overrun%1_000 == 0 {
			s.log.Warn("lane receipt queue overflowed",
				zap.String("subject", s.subject),
				zap.Int64("overrun_total", overrun))
		}
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

// deliver hands one message to the lane channel with its verdict already wired
// up. The reconciliation is a callback rather than a goroutine parked on
// Acked/Nacked, because at lane rate that goroutine and its 30-second timer were
// a spawn and a timer-heap insert per message for an outcome that, in the common
// case, does nothing at all: an ACK is receipt-level here and needs no traffic,
// since the flow-control responses already own the ack floor. Only a NACK costs
// anything, and it costs one scheduled retry.
//
// Two consequences are deliberate. The retry now runs on the handler's own
// goroutine, bounded by flowRetryTimeout, which is affordable because failure is
// rare by contract on these lanes. And a handler that resolves neither way holds
// its pending count until shutdown, where subscriberDrainTimeout bounds the wait
// — the same 30 seconds the per-message timer used to spend, now paid once at
// shutdown instead of being armed on every delivery.
func (s *flowSubscriber) deliver(delivery flowDelivery) bool {
	// Both the count and the handler are in place before the message is visible
	// to a handler: it can be resolved the instant the send completes.
	s.pending.Add(1)
	delivery.msg.setResolveHandler(func(acked bool) {
		defer s.pending.Done()
		if !acked {
			s.scheduleRetry(delivery)
		}
	})
	select {
	case s.output <- delivery.msg:
		return true
	case <-s.closeCh:
		// Nobody took the message, so the callback will never run and the count
		// it owns has to be released here or shutdown waits out its whole budget.
		s.pending.Done()
		return false
	}
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
