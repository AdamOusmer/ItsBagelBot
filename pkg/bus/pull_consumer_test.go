package bus

import (
	"context"
	"errors"
	"fmt"
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

	requireContract(t,
		// A floor ack costs one publish per batch; explicit acks would cost one per
		// message, which is the ack stream this lane shape exists to avoid.
		contractClause{cfg.AckPolicy == jsapi.AckAllPolicy,
			fmt.Sprintf("ack policy = %v, want AckAll", cfg.AckPolicy)},
		// A pull consumer must carry no delivery subject: one would make it a push
		// consumer and the server would stop honouring MSG.NEXT for it.
		contractClause{cfg.DeliverSubject == "" && !cfg.FlowControl,
			fmt.Sprintf("pull consumer was given push delivery: %#v", cfg)},
		contractClause{cfg.DeliverPolicy == jsapi.DeliverNewPolicy,
			fmt.Sprintf("deliver policy = %v, want DeliverNew on a first creation", cfg.DeliverPolicy)},
		// R1 memory consumer state on an R3 stream: replicating per-consumer ack
		// state is leader RAFT work on the hot path this design does not need.
		contractClause{cfg.Replicas == 1 && cfg.MemoryStorage,
			fmt.Sprintf("consumer state must be R1 in memory: %#v", cfg)},
		contractClause{cfg.InactiveThreshold == flowInactiveThreshold,
			fmt.Sprintf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)},
		contractClause{cfg.AckWait == defaultPullAckWait && cfg.MaxAckPending == defaultPullMaxAckPending,
			fmt.Sprintf("ack budget = %v/%d, want the shipped defaults", cfg.AckWait, cfg.MaxAckPending)},
	)
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

	batch := deliverPullBatch(t, sub, 1, 2, 3)
	// Nothing is acked until the batch is done: the floor is what makes the
	// per-message ack unnecessary, so a per-message ack here would be the bug.
	requireNoAcksYet(t, batch)

	sub.advanceFloor()
	requireFloorAckedOnce(t, batch)

	// The floor is published once per receipt: a second flush with nothing new
	// received must not re-ack a sequence the server already has.
	sub.advanceFloor()
	if last := batch[len(batch)-1]; last.acks() != 1 {
		t.Fatalf("floor was re-published: acks = %d", last.acks())
	}
	close(sub.closeCh)
	drain()
}

// deliverPullBatch hands the subscriber one fetch batch through the real deliver
// path, in sequence order.
func deliverPullBatch(t *testing.T, sub *pullSubscriber, sequences ...uint64) []*fakePullMsg {
	t.Helper()
	batch := make([]*fakePullMsg, 0, len(sequences))
	for _, sequence := range sequences {
		wire := fakePullDelivery(sequence)
		if !sub.deliver(wire) {
			t.Fatal("delivery was refused while the binding was open")
		}
		batch = append(batch, wire)
	}
	return batch
}

func requireNoAcksYet(t *testing.T, batch []*fakePullMsg) {
	t.Helper()
	for _, wire := range batch {
		if wire.acks() != 0 {
			t.Fatalf("message %d was acked individually", wire.sequence)
		}
	}
}

// requireFloorAckedOnce states the cadence: one publish for the whole batch,
// naming its last message and no other.
func requireFloorAckedOnce(t *testing.T, batch []*fakePullMsg) {
	t.Helper()
	last := batch[len(batch)-1]
	if last.acks() != 1 {
		t.Fatalf("last-of-batch acks = %d, want exactly one floor ack", last.acks())
	}
	for _, wire := range batch[:len(batch)-1] {
		if wire.acks() != 0 {
			t.Fatal("AckAll floor was published per message instead of per batch")
		}
	}
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
	hot := subscriptionTarget{stream: TwitchIngressStandardStream.Name, topic: "twitch.ingress.event.standard"}
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

// TestPullBindingReplacesThePushDurableOnTheModeFlip covers the transition the
// shared consumer name makes unavoidable. durableName(group, subject) carries no
// mode token, so flipping NATS_CONSUME_MODE to pull re-provisions the very
// durable the explicit push consumer occupies — and nats-server refuses to
// convert a push consumer to a pull one in place. Without the replacement the
// flip fails every pod's lane binding identically and the lane just stops
// consuming, which is the least visible way a lane can break.
func TestPullBindingReplacesThePushDurableOnTheModeFlip(t *testing.T) {
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100)}
	name := js.live.Config.Name

	consumer, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", name),
	)
	if err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if consumer == nil {
		t.Fatal("conversion returned no consumer to fetch from")
	}
	if js.deletes != 1 {
		t.Fatalf("deletes = %d, want exactly one: the conversion is the only thing that earns a delete", js.deletes)
	}
	if len(js.created) != 1 {
		t.Fatalf("creates = %d, want one recreation", len(js.created))
	}

	got := js.created[0]
	requireContract(t,
		contractClause{got.DeliverSubject == "",
			fmt.Sprintf("replacement carries delivery subject %q; it is still a push consumer", got.DeliverSubject)},
		contractClause{got.AckPolicy == jsapi.AckAllPolicy,
			fmt.Sprintf("replacement ack policy = %v, want the pull lane's floor-based AckAll", got.AckPolicy)},
		// The fleet's acknowledged position survives the delete: resuming anywhere
		// earlier re-executes chat commands the previous consumer already handled.
		contractClause{got.DeliverPolicy == jsapi.DeliverByStartSequencePolicy && got.OptStartSeq == 9_101,
			fmt.Sprintf("replacement resumed at %v/%d, want the predecessor's ack floor + 1",
				got.DeliverPolicy, got.OptStartSeq)},
	)
}

func TestPullReplacementNeverOpensOnTheWholeRetainedFirehose(t *testing.T) {
	// A push durable that never acked anything. Inheriting its DeliverAll would
	// replay every retained event to the converted lane at once.
	js := &pullConsumerSpy{live: livePushLaneConsumer(0)}

	if _, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	); err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if got := js.created[0]; got.DeliverPolicy != jsapi.DeliverNewPolicy || got.OptStartSeq != 0 {
		t.Fatalf("unknown ack floor resumed at %v/%d, want DeliverNew", got.DeliverPolicy, got.OptStartSeq)
	}
}

// TestPullBindingBindsAConversionAnotherPodAlreadyMade is the guard on the
// difference between the two receipt-level paths. A flow consumer is per-pod, so
// replacing one costs the caller its own cursor; this durable is fleet-wide, so
// a delete takes it out from under every other pod fetching from it. On a
// simultaneous fleet restart every pod would otherwise delete the successor the
// previous one just built.
func TestPullBindingBindsAConversionAnotherPodAlreadyMade(t *testing.T) {
	// The conversion lands between our INFO and our second update.
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100), convertAfter: 2}

	consumer, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	)
	if err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if consumer == nil {
		t.Fatal("raced conversion returned no consumer to fetch from")
	}
	if js.deletes != 0 {
		t.Fatalf("deletes = %d, want none: the durable was already converted", js.deletes)
	}
}

func TestPullBindingNeverDeletesOnATransientFailure(t *testing.T) {
	// No responders during a meta election is the canonical one: deleting here
	// would turn a retryable blip into a fleet-wide delivery reset.
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100), createErr: nats.ErrNoResponders}

	_, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	)
	if err == nil {
		t.Fatal("a transient provisioning failure was swallowed")
	}
	if js.deletes != 0 {
		t.Fatalf("deletes = %d, want none: only an immutable-field rejection earns a delete", js.deletes)
	}
}

// ---- fakes -----------------------------------------------------------------

// livePushLaneConsumer models the explicit-ACK durable a lane runs before the
// mode flip: push (it has a delivery subject), per-message acks, opened at
// DeliverAll.
func livePushLaneConsumer(ackFloor uint64) *jsapi.ConsumerInfo {
	name := "worker_twitch_ingress_event_standard"
	info := &jsapi.ConsumerInfo{
		Config: jsapi.ConsumerConfig{
			Name:           name,
			Durable:        name,
			DeliverPolicy:  jsapi.DeliverAllPolicy,
			AckPolicy:      jsapi.AckExplicitPolicy,
			FilterSubject:  "twitch.ingress.event.standard",
			DeliverSubject: "_INBOX.BAGEL." + subjectToken(name),
			DeliverGroup:   "worker",
		},
	}
	info.AckFloor.Stream = ackFloor
	return info
}

// pullConsumerSpy stands in for the JetStream API the pull binding uses,
// modelling the one server behaviour the replacement path exists for: a live
// push consumer cannot be converted in place, and only a delete makes room.
type pullConsumerSpy struct {
	live      *jsapi.ConsumerInfo
	createErr error
	// convertAfter simulates another pod completing the conversion, on the Nth
	// create attempt.
	convertAfter int

	attempts int
	created  []jsapi.ConsumerConfig
	deletes  int
}

// pullConsumerHandle satisfies jetstream.Consumer by embedding the interface, so
// any method the binding does not call panics instead of returning a zero value.
type pullConsumerHandle struct {
	jsapi.Consumer
	info *jsapi.ConsumerInfo
}

func (c *pullConsumerHandle) Info(context.Context) (*jsapi.ConsumerInfo, error) {
	return c.info, nil
}

func (s *pullConsumerSpy) Consumer(context.Context, string, string) (jsapi.Consumer, error) {
	if s.live == nil {
		return nil, jsapi.ErrConsumerNotFound
	}
	// The real client refuses to describe a push consumer through the pull
	// accessor; modelling that here is what keeps these tests honest about
	// the conversion (the fake used to hand the info back and the flip
	// failed in production on this exact lookup).
	if s.live.Config.DeliverSubject != "" {
		return nil, jsapi.ErrNotPullConsumer
	}
	return &pullConsumerHandle{info: s.live}, nil
}

// pushConsumerHandle mirrors pullConsumerHandle for the push accessor.
type pushConsumerHandle struct {
	jsapi.PushConsumer
	info *jsapi.ConsumerInfo
}

func (c *pushConsumerHandle) Info(context.Context) (*jsapi.ConsumerInfo, error) {
	return c.info, nil
}

func (s *pullConsumerSpy) PushConsumer(context.Context, string, string) (jsapi.PushConsumer, error) {
	if s.live == nil {
		return nil, jsapi.ErrConsumerNotFound
	}
	if s.live.Config.DeliverSubject == "" {
		return nil, jsapi.ErrNotPushConsumer
	}
	return &pushConsumerHandle{info: s.live}, nil
}

func (s *pullConsumerSpy) CreateOrUpdateConsumer(
	_ context.Context,
	_ string,
	cfg jsapi.ConsumerConfig,
) (jsapi.Consumer, error) {
	s.attempts++
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.racedConversionLanded() {
		s.live.Config.DeliverSubject = ""
		s.live.Config.MemoryStorage = true
	}
	if err := s.refusesImmutableUpdate(cfg); err != nil {
		return nil, err
	}
	s.created = append(s.created, cfg)
	s.live = &jsapi.ConsumerInfo{Config: cfg}
	return &pullConsumerHandle{info: s.live}, nil
}

// racedConversionLanded plays the other pod: after the configured number of
// attempts, the live push durable has already been converted underneath us.
func (s *pullConsumerSpy) racedConversionLanded() bool {
	return s.convertAfter > 0 && s.attempts >= s.convertAfter && s.live != nil
}

// refusesImmutableUpdate is the server rule the whole conversion path exists
// for, in checkNewConsumerConfig's own field order: storage type is checked
// before the push/pull deliver subject, so the storage message — wrapped the
// way the client wraps an API error — is what a real flip surfaces first
// (production, 2026-08-15). The conversion message only appears when the
// storage types happen to agree.
func (s *pullConsumerSpy) refusesImmutableUpdate(cfg jsapi.ConsumerConfig) error {
	if s.live == nil {
		return nil
	}
	if s.live.Config.MemoryStorage != cfg.MemoryStorage {
		return errors.New("nats: API error: code=500 err_code=10012 description=storage type can not be updated")
	}
	if s.live.Config.DeliverSubject != "" && cfg.DeliverSubject == "" {
		return errors.New("nats: can not update push consumer to pull based")
	}
	return nil
}

func (s *pullConsumerSpy) DeleteConsumer(context.Context, string, string) error {
	s.deletes++
	s.live = nil
	return nil
}

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
