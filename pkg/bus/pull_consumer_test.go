package bus

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

func TestPullConsumerIsOneSharedDurableForTheWholeFleet(t *testing.T) {
	t.Setenv("POD_NAME", "sesame-6d9f7c8b45-tq2xz")
	name := pullConsumerName("worker", "twitch.ingress.event.premium")

	// The absence of pod identity IS the design: every pod binding the same
	// durable is what makes the server distribute the lane instead of copying it.
	if name != durableName("worker", "twitch.ingress.event.premium") {
		t.Fatalf("consumer name = %q, want the plain fleet-wide durable", name)
	}
	if strings.Contains(name, "tq2xz") || strings.Contains(name, podIdentity()) {
		t.Fatalf("consumer name %q carries pod identity and would fan the lane out", name)
	}
	// The flow lane's per-pod name is the shape this one exists to replace.
	if name == flowConsumerName("worker", "twitch.ingress.event.premium") {
		t.Fatal("pull and flow durables collide")
	}
}

func TestPullConsumerConfigIsCheapFloorAcknowledgement(t *testing.T) {
	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_twitch_ingress_event_premium")

	// A floor ack costs one publish per batch; explicit acks would cost one per
	// message, which is the ack stream this lane shape exists to avoid.
	if cfg.AckPolicy != jsapi.AckAllPolicy {
		t.Fatalf("ack policy = %v, want AckAll", cfg.AckPolicy)
	}
	// A pull consumer must carry no delivery subject: one would make it a push
	// consumer and the server would stop honouring MSG.NEXT for it.
	if cfg.DeliverSubject != "" || cfg.FlowControl {
		t.Fatalf("pull consumer was given push delivery: %#v", cfg)
	}
	if cfg.DeliverPolicy != jsapi.DeliverNewPolicy {
		t.Fatalf("deliver policy = %v, want DeliverNew on a first creation", cfg.DeliverPolicy)
	}
	// R1 memory consumer state on an R3 stream: replicating per-consumer ack
	// state is leader RAFT work on the hot path this design does not need.
	if cfg.Replicas != 1 || !cfg.MemoryStorage {
		t.Fatalf("consumer state must be R1 in memory: %#v", cfg)
	}
	if cfg.InactiveThreshold != flowInactiveThreshold {
		t.Fatalf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)
	}
	if cfg.AckWait != defaultPullAckWait || cfg.MaxAckPending != defaultPullMaxAckPending {
		t.Fatalf("ack budget = %v/%d, want the shipped defaults", cfg.AckWait, cfg.MaxAckPending)
	}
}

func TestPullConsumerKnobsRejectNonPositiveOverrides(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_WAIT", "45s")
	t.Setenv("NATS_PULL_MAX_ACK_PENDING", "70000")
	t.Setenv("NATS_PULL_FETCH_BATCH", "0")
	t.Setenv("NATS_PULL_FETCH_MAXWAIT", "-1s")
	t.Setenv("NATS_PULL_ACK_EVERY", "100ms")

	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_premium")
	if cfg.AckWait != 45*time.Second || cfg.MaxAckPending != 70000 {
		t.Fatalf("valid overrides were ignored: %v/%d", cfg.AckWait, cfg.MaxAckPending)
	}
	// Every knob is a rate or a ceiling, so a zero or negative one is a manifest
	// typo rather than an instruction: it must not reshape the consumer.
	if pullFetchBatch() != defaultPullFetchBatch || pullFetchMaxWait() != defaultPullFetchMaxWait {
		t.Fatalf("non-positive override was accepted: batch=%d wait=%v",
			pullFetchBatch(), pullFetchMaxWait())
	}
	if pullAckEvery() != 100*time.Millisecond {
		t.Fatalf("ack cadence = %v, want the override", pullAckEvery())
	}
}

// TestPullFloorAckCoversTheWholeBatch is the ack-cadence contract: one publish
// for every message the batch handed out, naming the last of them.
func TestPullFloorAckCoversTheWholeBatch(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	batch := []*fakePullMsg{fakePullDelivery(1), fakePullDelivery(2), fakePullDelivery(3)}
	for _, wire := range batch {
		if !sub.deliver(wire) {
			t.Fatal("delivery was refused while the binding was open")
		}
	}
	// Nothing is acked until the batch is done: the floor is what makes the
	// per-message ack unnecessary, so a per-message ack here would be the bug.
	for _, wire := range batch {
		if wire.acks() != 0 {
			t.Fatalf("message %d was acked individually", wire.sequence)
		}
	}

	sub.advanceFloor()
	if batch[2].acks() != 1 {
		t.Fatalf("last-of-batch acks = %d, want exactly one floor ack", batch[2].acks())
	}
	if batch[0].acks() != 0 || batch[1].acks() != 0 {
		t.Fatal("AckAll floor was published per message instead of per batch")
	}

	// The floor is published once per receipt: a second flush with nothing new
	// received must not re-ack a sequence the server already has.
	sub.advanceFloor()
	if batch[2].acks() != 1 {
		t.Fatalf("floor was re-published: acks = %d", batch[2].acks())
	}
	close(sub.closeCh)
	drain()
}

// TestPullFloorAckTimerCoversAPartiallyDrainedBatch is the other half of the
// cadence. A pod blocked on a slow handler pool must not hold the shared pending
// set open for the messages it has already handed out.
func TestPullFloorAckTimerCoversAPartiallyDrainedBatch(t *testing.T) {
	sub := testPullSubscriber()
	sub.ackEvery = 5 * time.Millisecond
	drain := drainLane(sub)

	first, second := fakePullDelivery(7), fakePullDelivery(8)
	sub.deliver(first)
	sub.deliver(second)

	sub.workers.Add(1)
	go sub.advanceFloorPeriodically()

	waitFor(t, func() bool { return second.acks() == 1 }, "the ack timer never advanced the floor")
	if first.acks() != 0 {
		t.Fatal("the timer acked per message instead of publishing one floor")
	}
	close(sub.closeCh)
	sub.workers.Wait()
	drain()
}

// TestPullCloseAcksTheFinalFloor guards the shutdown path: a pod that exits
// without publishing its floor hands its whole last batch back to the fleet.
func TestPullCloseAcksTheFinalFloor(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	last := fakePullDelivery(42)
	sub.deliver(last)

	// The real shutdown flushes and closes the connection; this asserts the step
	// that has to happen before it, in the order it happens.
	close(sub.closeCh)
	sub.advanceFloor()
	if last.acks() != 1 {
		t.Fatalf("final floor acks = %d, want one", last.acks())
	}
	drain()
}

func TestPullMalformedDeliveryStillAdvancesTheFloor(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	wire := fakePullDelivery(11)
	// A multi-valued header is what the decoder rejects; the delivery still
	// happened, and a hole in the floor for it would stall the shared durable at
	// MaxAckPending for every pod on the lane.
	wire.header["Bagelbot-Lane"] = []string{"premium", "standard"}

	if !sub.deliver(wire) {
		t.Fatal("a malformed delivery closed the binding")
	}
	sub.advanceFloor()
	if wire.acks() != 1 {
		t.Fatalf("malformed delivery acks = %d, want the floor to move past it", wire.acks())
	}
}

// TestPullNackUsesTheSharedRetryHelper proves the pull path reaches the same
// one-hop budget check the flow path does, rather than a second copy of it.
func TestPullNackUsesTheSharedRetryHelper(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	msg := NewMessage("event-1", []byte("{}"))
	msg.Metadata.Set(RetryCountHeader, "1")
	delivery := pullDelivery{wire: nats.NewMsg(sub.subject), msg: msg}

	// The budget is exhausted, so the helper refuses before it ever touches the
	// connection — which is also what lets this run without a broker.
	sub.scheduleRetry(delivery)
	if sub.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want the exhausted retry budget to drop the event", sub.dropped.Load())
	}
	if err := scheduleLaneRetry(nil, sub.subject, delivery.wire, msg); err == nil ||
		!strings.Contains(err.Error(), "retry budget exhausted") {
		t.Fatalf("shared helper error = %v, want the one-hop budget refusal", err)
	}
}

// TestPullNackReachesTheRetryPathFromTheResolveCallback runs the verdict through
// the real deliver path. There is no goroutine parked on the message's signals
// any more, so the callback deliver installs is the only thing that can carry a
// failure to the retry helper.
func TestPullNackReachesTheRetryPathFromTheResolveCallback(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	wire := fakePullDelivery(12)
	// An exhausted budget refuses inside the shared helper before it reaches the
	// connection, which is what lets the retry path run here without a broker.
	wire.header.Set(RetryCountHeader, "1")

	received := make(chan *Message, 1)
	go func() { received <- <-sub.output }()
	if !sub.deliver(wire) {
		t.Fatal("delivery was refused while the binding was open")
	}

	msg := <-received
	if !msg.Nack() {
		t.Fatal("the first Nack must win")
	}
	// A losing call must not schedule the event a second time.
	msg.Nack()

	sub.inflight.Wait()
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want exactly one retry attempt",
			sub.dropped.Load(), sub.retried.Load())
	}
}

// TestPullDeliveryLostToShutdownReleasesItsInflightCount guards the one path
// where the resolve callback provably never runs: the send lost the race to
// closeCh, so no handler ever saw the message. Without the explicit release,
// shutdown would spend its whole drain budget on a count nobody owns.
func TestPullDeliveryLostToShutdownReleasesItsInflightCount(t *testing.T) {
	sub := testPullSubscriber()
	// Nothing reads the lane channel and the binding is already closing.
	close(sub.closeCh)

	if sub.deliver(fakePullDelivery(5)) {
		t.Fatal("a delivery was accepted after the binding closed")
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	if !waitGroupBefore(&sub.inflight, deadline.C) {
		t.Fatal("a delivery lost to shutdown leaked its inflight count")
	}
}

// TestPullWireCarriesAStableIdentity guards the retry hop. The pull API's
// message cannot be read through nats.go's subscription-bound metadata parser,
// so without the stamp an ingress-origin event would get a fresh NUID per
// delivery and its retry would be unmatchable by any dedup guard.
func TestPullWireCarriesAStableIdentity(t *testing.T) {
	wire := fakePullDelivery(99)
	first := pullWireMessage(wire)
	second := pullWireMessage(fakePullDelivery(99))

	want := jetStreamIdentity("hub", TwitchIngressStream.Name, 99)
	if got := first.Header.Get(MessageIDHeader); got != want {
		t.Fatalf("stamped identity = %q, want %q", got, want)
	}
	if second.Header.Get(MessageIDHeader) != want {
		t.Fatal("two deliveries of the same sequence got different identities")
	}

	// A publisher-set id is never overwritten: it is the fleet's own identity and
	// outranks the derived one.
	authored := fakePullDelivery(100)
	authored.header.Set(MessageIDHeader, "authored-id")
	if got := pullWireMessage(authored).Header.Get(MessageIDHeader); got != "authored-id" {
		t.Fatalf("publisher identity was overwritten with %q", got)
	}
}

func TestPullSubscriberIsBoundToOneSubject(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	first, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	// One fetch loop per pod means one delivery stream per pod: the units compete
	// for the same channel instead of each opening its own fetch loop.
	if first != second {
		t.Fatal("a second consumer unit was handed its own lane channel")
	}
	if _, err := sub.Subscribe(context.Background(), "twitch.ingress.event.premium"); err == nil {
		t.Fatal("the subscriber accepted a subject it is not bound to")
	}
	sub.closed.Store(true)
	if _, err := sub.Subscribe(context.Background(), sub.subject); err == nil {
		t.Fatal("a closed subscriber handed out its lane channel")
	}
}

func TestConsumeModeIsThreeWayAndDefaultsToPull(t *testing.T) {
	for _, test := range []struct {
		name string
		flow string
		mode string
		want laneConsumeMode
	}{
		// Unset is the deployed state: receipt-level modes are opt-in, so an
		// unconfigured lane keeps per-message explicit acks whatever the mode says.
		{"unset", "", "", laneModeExplicit},
		{"mode without the opt-in", "", "pull", laneModeExplicit},
		{"flow", "on", "flow", laneModeFlow},
		{"pull", "on", "pull", laneModePull},
		{"explicit", "on", "explicit", laneModeExplicit},
		// A typo must not silently reshape the lane away from the default.
		{"garbage", "on", "puull", laneModePull},
		// Unset mode with the opt-in takes the pull default: the fan-out flow
		// shape multiplies delivery per pod, pull divides it.
		{"default", "on", "", laneModePull},
		// NATS_CONSUME_FLOW=off is the deployed kill switch and outranks the mode
		// outright: an operator reaching for it must not have to know that a
		// second variable exists.
		{"backcompat off", "off", "pull", laneModeExplicit},
		{"backcompat off over flow", "off", "flow", laneModeExplicit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NATS_CONSUME_FLOW", test.flow)
			t.Setenv("NATS_CONSUME_MODE", test.mode)
			if got := consumeMode(); got != test.want {
				t.Fatalf("consumeMode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPullModeStillRefusesLanesOutsideTheHotIngress(t *testing.T) {
	t.Setenv("NATS_CONSUME_FLOW", "on")
	t.Setenv("NATS_CONSUME_MODE", "pull")
	subscriber := &fleetSubscriber{group: "worker"}
	hot := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.event.standard"}
	control := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.status.authz.revoked"}

	if got := subscriber.laneModeFor(hot); got != laneModePull {
		t.Fatalf("hot lane mode = %q, want pull", got)
	}
	// The scope guard is unchanged by the new mode: replay-sensitive lanes keep
	// per-message explicit acks whichever receipt-level mode is configured.
	if got := subscriber.laneModeFor(control); got != laneModeExplicit {
		t.Fatalf("control lane mode = %q, want explicit", got)
	}
}

// ---- fakes -----------------------------------------------------------------

func testPullSubscriber() *pullSubscriber {
	return &pullSubscriber{
		stream: TwitchIngressStream.Name, subject: "twitch.ingress.event.standard",
		name:     "worker_twitch_ingress_event_standard",
		log:      zap.NewNop(),
		batch:    defaultPullFetchBatch,
		maxWait:  defaultPullFetchMaxWait,
		ackEvery: defaultPullAckEvery,
		output:   make(chan *Message),
		closeCh:  make(chan struct{}),
	}
}

// drainLane takes deliveries off the lane channel the way a consumer unit would,
// so deliver() is never the thing blocking a test. The returned function waits
// for the reader to finish, which the caller does after closing closeCh.
func drainLane(sub *pullSubscriber) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-sub.output:
			case <-sub.closeCh:
				return
			}
		}
	}()
	return func() { <-done }
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}

// fakePullMsg stands in for one jetstream delivery. It counts acks rather than
// publishing them, which is what makes the cadence assertions possible without a
// broker.
type fakePullMsg struct {
	sequence uint64
	header   nats.Header
	payload  []byte

	mu     sync.Mutex
	acked  int
	nakked int
}

func fakePullDelivery(sequence uint64) *fakePullMsg {
	return &fakePullMsg{
		sequence: sequence,
		header:   nats.Header{"Bagelbot-Lane": []string{"standard"}},
		payload:  []byte(`{"event":"chat"}`),
	}
}

func (m *fakePullMsg) acks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *fakePullMsg) Metadata() (*jsapi.MsgMetadata, error) {
	return &jsapi.MsgMetadata{
		Domain:   "hub",
		Stream:   TwitchIngressStream.Name,
		Consumer: "worker_twitch_ingress_event_standard",
		Sequence: jsapi.SequencePair{Stream: m.sequence, Consumer: m.sequence},
	}, nil
}

func (m *fakePullMsg) Data() []byte         { return m.payload }
func (m *fakePullMsg) Headers() nats.Header { return m.header }
func (m *fakePullMsg) Subject() string      { return "twitch.ingress.event.standard" }
func (m *fakePullMsg) Reply() string        { return "$JS.ACK.hub.x.TWITCH_INGRESS.c.1.1.1.0.0" }
func (m *fakePullMsg) DoubleAck(context.Context) error {
	// Never reachable: a quorum round trip per message is the cost this lane
	// exists to avoid, so a test that hits this has found a real regression.
	panic("pull lane must never double-ack")
}
func (m *fakePullMsg) NakWithDelay(time.Duration) error { return m.Nak() }
func (m *fakePullMsg) InProgress() error                { return nil }
func (m *fakePullMsg) Term() error                      { return nil }
func (m *fakePullMsg) TermWithReason(string) error      { return nil }

func (m *fakePullMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked++
	return nil
}

func (m *fakePullMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nakked++
	return nil
}
