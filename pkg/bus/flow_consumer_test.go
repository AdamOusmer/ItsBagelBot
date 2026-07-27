package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

func TestOnlyHotIngressLanesUseFlowControl(t *testing.T) {
	for _, test := range []struct {
		stream  string
		subject string
		want    bool
	}{
		{TwitchIngressStream.Name, "twitch.ingress.event.premium", true},
		{TwitchIngressStream.Name, "twitch.ingress.event.standard", true},
		{TwitchIngressStream.Name, "twitch.ingress.event.stream", false},
		{TwitchIngressStream.Name, "twitch.ingress.status.authz.revoked", false},
		{OutgressStream.Name, "twitch.outgress.standard", false},
		{BagelDataStream.Name, "data.users.updated", false},
	} {
		if got := isHotIngressLane(test.stream, test.subject); got != test.want {
			t.Fatalf("isHotIngressLane(%q, %q) = %v, want %v", test.stream, test.subject, got, test.want)
		}
	}
}

func TestFlowConsumerConfigIsAcceptedByTheServerContract(t *testing.T) {
	cfg := flowConsumerConfig("twitch.ingress.event.premium", "worker_premium_pod_1")

	if cfg.AckPolicy != jsapi.AckFlowControlPolicy {
		t.Fatalf("ack policy = %v, want AckFlowControl", cfg.AckPolicy)
	}
	// The server rejects an AckFlowControl consumer that is not a push consumer,
	// has flow control off, or uses any heartbeat other than one second.
	if !cfg.FlowControl || cfg.DeliverSubject == "" || cfg.IdleHeartbeat != time.Second {
		t.Fatalf("flow contract violated: %#v", cfg)
	}
	if cfg.MaxAckPending != flowMaxAckPending {
		t.Fatalf("max ack pending = %d, want %d", cfg.MaxAckPending, flowMaxAckPending)
	}
	if cfg.Replicas != 0 || !cfg.MemoryStorage {
		t.Fatalf("consumer state must inherit stream replicas in memory: %#v", cfg)
	}
	// A first creation must not replay the retained firehose.
	if cfg.DeliverPolicy != jsapi.DeliverNewPolicy {
		t.Fatalf("deliver policy = %v, want DeliverNew", cfg.DeliverPolicy)
	}
}

func TestFlowConsumerIsSingleSubscriberPerPod(t *testing.T) {
	t.Setenv("POD_NAME", "sesame-6d9f7c8b45-tq2xz")
	name := flowConsumerName("worker", "twitch.ingress.event.premium")
	cfg := flowConsumerConfig("twitch.ingress.event.premium", name)

	// A queue group would hand one arbitrary member every flow-control request,
	// and its cumulative answer would acknowledge work still running elsewhere.
	if cfg.DeliverGroup != "" {
		t.Fatalf("flow consumer bound a queue group: %q", cfg.DeliverGroup)
	}
	if !strings.HasSuffix(name, "sesame-6d9f7c8b45-tq2xz") {
		t.Fatalf("consumer name = %q, want this pod's identity", name)
	}
	if cfg.DeliverSubject != "_INBOX.BAGEL."+subjectToken(name) {
		t.Fatalf("delivery subject = %q, want one derived from the pod's consumer", cfg.DeliverSubject)
	}
	// A pod that never returns leaves its consumer behind; the server deletes it
	// once the delivery subject has had no interest for this long.
	if cfg.InactiveThreshold != flowInactiveThreshold {
		t.Fatalf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)
	}
}

func TestPodIdentityFallsBackAndStaysNameSafe(t *testing.T) {
	t.Setenv("POD_NAME", "")
	t.Setenv("HOSTNAME", "sesame.node-3.local")
	if got := podIdentity(); got != "sesame_node-3_local" {
		t.Fatalf("pod identity = %q, want the dots replaced", got)
	}
	if got := consumerToken(strings.Repeat("x", 200)); len(got) != 48 {
		t.Fatalf("token length = %d, want the durable bounded at 48", len(got))
	}
	if got := consumerToken("pod*name>with.wildcards"); strings.ContainsAny(got, ".*>") {
		t.Fatalf("token %q still carries subject wildcards", got)
	}
}

func TestFlowConsumerRecoveryResumesAfterTheReceiptCursor(t *testing.T) {
	cfg := recoveryFlowConsumerConfig(
		"twitch.ingress.event.standard", "worker_standard_pod_1",
		flowPosition{consumer: 90, stream: 120},
	)
	if cfg.DeliverPolicy != jsapi.DeliverByStartSequencePolicy || cfg.OptStartSeq != 121 {
		t.Fatalf("recovery policy=%v start=%d, want start sequence 121", cfg.DeliverPolicy, cfg.OptStartSeq)
	}

	fresh := recoveryFlowConsumerConfig("twitch.ingress.event.standard", "worker_standard_pod_1", flowPosition{})
	if fresh.DeliverPolicy != jsapi.DeliverNewPolicy {
		t.Fatalf("empty cursor recovery policy = %v, want DeliverNew", fresh.DeliverPolicy)
	}
}

func TestCarriedAckFloorSurvivesConsumerReplacement(t *testing.T) {
	desired := flowConsumerConfig("twitch.ingress.event.premium", "worker_premium_pod_1")
	info := &jsapi.ConsumerInfo{}
	info.AckFloor.Stream = 4_200

	carryFlowAckFloor(&desired, info)
	if desired.DeliverPolicy != jsapi.DeliverByStartSequencePolicy || desired.OptStartSeq != 4_201 {
		t.Fatalf("replacement resumed at %v/%d", desired.DeliverPolicy, desired.OptStartSeq)
	}
}

func TestUnknownAckFloorNeverInheritsThePredecessorPolicy(t *testing.T) {
	desired := flowConsumerConfig("twitch.ingress.event.premium", "worker_premium_pod_1")
	// The explicit-ACK consumer this replaces is DeliverAll; inheriting it would
	// open the replacement on the whole retained firehose.
	desired.DeliverPolicy = jsapi.DeliverAllPolicy
	desired.OptStartSeq = 77

	carryFlowAckFloor(&desired, &jsapi.ConsumerInfo{})
	if desired.DeliverPolicy != jsapi.DeliverNewPolicy || desired.OptStartSeq != 0 {
		t.Fatalf("zero ack floor resumed at %v/%d, want DeliverNew", desired.DeliverPolicy, desired.OptStartSeq)
	}
}

func TestOnlyImmutableFieldErrorsReplaceTheConsumer(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{errors.New("nats: ack policy can not be updated"), true},
		{errors.New("nats: flow control can not be updated"), true},
		{errors.New("nats: heart beats can not be updated"), true},
		{context.DeadlineExceeded, false},
		{nats.ErrNoResponders, false},
		{jsapi.ErrConsumerNotFound, false},
		{errors.New("nats: max waiting can not be updated"), false},
		{nil, false},
	} {
		if got := requiresConsumerReplacement(test.err); got != test.want {
			t.Fatalf("requiresConsumerReplacement(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}

func TestFlowControlResponseCarriesTheReceiptCursor(t *testing.T) {
	control := flowStatus("FlowControl Request")
	control.Reply = "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"

	response := flowControlResponse(control, flowPosition{consumer: 42, stream: 99})
	if response == nil {
		t.Fatal("flow-control request produced no response")
	}
	if response.Subject != control.Reply {
		t.Fatalf("response subject = %q, want %q", response.Subject, control.Reply)
	}
	if response.Header.Get(lastConsumerHeader) != "42" || response.Header.Get(lastStreamHeader) != "99" {
		t.Fatalf("response cursor = %#v", response.Header)
	}
}

func TestIdleHeartbeatIsAnsweredOnlyWhileStalled(t *testing.T) {
	quiet := flowStatus("Idle Heartbeat")
	quiet.Header.Set(lastConsumerHeader, "500")
	quiet.Header.Set(lastStreamHeader, "900")
	if response := flowControlResponse(quiet, flowPosition{consumer: 42, stream: 99}); response != nil {
		t.Fatalf("unstalled heartbeat produced response %#v", response)
	}

	stalled := flowStatus("Idle Heartbeat")
	stalled.Header.Set(consumerStalledHeader, "$JS.FC.TWITCH_INGRESS.worker_premium.stall")
	stalled.Header.Set(lastConsumerHeader, "500")
	stalled.Header.Set(lastStreamHeader, "900")

	response := flowControlResponse(stalled, flowPosition{consumer: 42, stream: 99})
	if response == nil || response.Subject != "$JS.FC.TWITCH_INGRESS.worker_premium.stall" {
		t.Fatalf("stalled heartbeat response = %#v", response)
	}
	// The server reports what it sent, not what arrived. Answering with the
	// server's cursor would advance the replicated ack floor past deliveries
	// this process never received.
	if response.Header.Get(lastConsumerHeader) != "42" || response.Header.Get(lastStreamHeader) != "99" {
		t.Fatalf("stalled response used the server cursor: %#v", response.Header)
	}
}

func TestFlowControlIsAnsweredBeforeAnyDelivery(t *testing.T) {
	control := flowStatus("FlowControl Request")
	control.Reply = "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"

	// The window only reopens on the response arriving, so an empty cursor must
	// still answer — it simply claims no ack floor.
	response := flowControlResponse(control, flowPosition{})
	if response == nil {
		t.Fatal("empty cursor produced no response")
	}
	if response.Header.Get(lastStreamHeader) != "" {
		t.Fatalf("empty cursor claimed an ack floor: %#v", response.Header)
	}
}

func TestReceiptCursorTracksTheStreamSequence(t *testing.T) {
	cursor := &flowCursor{}
	if !cursor.record(10, 100) {
		t.Fatal("first sample was refused")
	}
	// Stale samples inside the same session never move the floor backwards.
	if cursor.record(4, 40) {
		t.Fatal("a stale sample advanced the cursor")
	}
	if got := cursor.snapshot(); got.consumer != 10 || got.stream != 100 {
		t.Fatalf("cursor regressed to %+v", got)
	}
	if !cursor.record(11, 101) || cursor.snapshot().stream != 101 {
		t.Fatalf("cursor did not advance: %+v", cursor.snapshot())
	}
}

func TestReceiptCursorSurvivesAConsumerSequenceReset(t *testing.T) {
	cursor := &flowCursor{}
	cursor.record(5_000_000, 9_000_000)

	// A delete + recreate restarts the consumer sequence at 1 while the stream
	// sequence keeps climbing. Gating on the consumer sequence would reject every
	// delivery of the new session forever.
	if !cursor.record(1, 9_000_001) {
		t.Fatal("the recreated consumer's first delivery was refused")
	}
	if got := cursor.snapshot(); got.consumer != 1 || got.stream != 9_000_001 {
		t.Fatalf("cursor after reset = %+v", got)
	}

	// A recreation at an earlier start sequence moves both coordinates back: the
	// backwards jump in consumer sequence is the server's own reset signature.
	rewound := &flowCursor{}
	rewound.record(5_000_000, 9_000_000)
	if !rewound.record(2, 12) {
		t.Fatal("a session reset below the stored stream sequence was refused")
	}
	if got := rewound.snapshot(); got.stream != 12 {
		t.Fatalf("cursor after a rewound session = %+v", got)
	}
}

func TestReceiptCursorResetsOnSelfInitiatedRecreate(t *testing.T) {
	cursor := &flowCursor{}
	cursor.record(90, 120)
	cursor.reset()
	if got := cursor.snapshot(); got != (flowPosition{}) {
		t.Fatalf("cursor after reset = %+v, want empty", got)
	}
}

func TestRetryScheduleMatchesTheServerScheduleContract(t *testing.T) {
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	wire.Header.Set("Nats-Expected-Last-Sequence", "9")
	wire.Header.Set("traceparent", "00-trace-span-01")
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	schedule := retryScheduleMsg("twitch.ingress.event.premium", wire, 3*time.Second, now)

	if got := schedule.Header.Get(jsapi.ScheduleHeader); got != "@at 2026-07-27T12:00:03Z" {
		t.Fatalf("schedule pattern = %q, want a one-shot @at three seconds out", got)
	}
	if got := schedule.Header.Get(jsapi.ScheduleTargetHeader); got != "twitch.ingress.retry.premium" {
		t.Fatalf("schedule target = %q", got)
	}
	// The server rejects @at outright when a time zone header is present, even UTC.
	if schedule.Header.Get(jsapi.ScheduleTimeZoneHeader) != "" {
		t.Fatalf("schedule carried a time zone: %#v", schedule.Header)
	}
	if schedule.Header.Get(jsapi.ScheduleTTLHeader) != retryScheduleTTL {
		t.Fatalf("schedule TTL = %q", schedule.Header.Get(jsapi.ScheduleTTLHeader))
	}
	if schedule.Header.Get(RetryCountHeader) != "1" {
		t.Fatalf("retry count = %q, want the first hop", schedule.Header.Get(RetryCountHeader))
	}
	// Application headers survive the emit; Nats-* headers do not, and the
	// expectation headers among them would be re-evaluated against the retry
	// stream and reject the publish.
	if schedule.Header.Get("traceparent") != "00-trace-span-01" {
		t.Fatalf("application headers were dropped: %#v", schedule.Header)
	}
	if schedule.Header.Get(MessageIDHeader) != "logical-id" {
		t.Fatalf("fleet identity was dropped: %#v", schedule.Header)
	}
	if schedule.Header.Get("Nats-Expected-Last-Sequence") != "" {
		t.Fatalf("an expectation header rode the retry: %#v", schedule.Header)
	}
	if string(schedule.Data) != `{"text":"hello"}` {
		t.Fatalf("retry payload = %q", schedule.Data)
	}
}

func TestRetryStampsIdentityWhenIngressWireHasNone(t *testing.T) {
	// Ingress-origin events reach the lane with no Bagelbot-Message-Id, so the
	// re-emitted retry would otherwise land under a fresh stream sequence and read
	// as a different message. The retry must stamp the delivery's resolved
	// identity so a consumer's dedup guard recognises it as the same event.
	wire := nats.NewMsg("twitch.ingress.event.standard")
	wire.Data = []byte(`{"text":"hello"}`)
	if wire.Header.Get(MessageIDHeader) != "" {
		t.Fatal("precondition: an ingress wire carries no message id")
	}

	schedule := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())
	if schedule.Header.Get(MessageIDHeader) == "" {
		t.Fatalf("retry left the fleet identity empty: %#v", schedule.Header)
	}
}

func TestRetrySchedulesNeverShareASubject(t *testing.T) {
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	first := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())
	second := retryScheduleMsg("twitch.ingress.event.standard", wire, time.Second, time.Now())

	// Publishing a schedule rolls its subject up, so a shared subject would purge
	// the previous retry before it ever fired.
	if first.Subject == second.Subject {
		t.Fatalf("two retries shared the schedule subject %q", first.Subject)
	}
	for _, subject := range []string{first.Subject, second.Subject} {
		if !strings.HasPrefix(subject, "twitch.ingress.retry.standard.") {
			t.Fatalf("schedule subject %q left the retry lane's namespace", subject)
		}
	}
}

func TestRetryLaneSubjectIsTheScheduleTarget(t *testing.T) {
	if got := RetryLaneSubject("twitch.ingress.event.premium"); got != "twitch.ingress.retry.premium" {
		t.Fatalf("retry lane = %q", got)
	}
}

func TestRetryDelayIsTunable(t *testing.T) {
	t.Setenv("NATS_FLOW_RETRY_DELAY", "")
	if got := flowRetryDelay(); got != defaultFlowRetryDelay {
		t.Fatalf("default retry delay = %v", got)
	}
	t.Setenv("NATS_FLOW_RETRY_DELAY", "12s")
	if got := flowRetryDelay(); got != 12*time.Second {
		t.Fatalf("configured retry delay = %v", got)
	}
	t.Setenv("NATS_FLOW_RETRY_DELAY", "-1s")
	if got := flowRetryDelay(); got != defaultFlowRetryDelay {
		t.Fatalf("negative retry delay = %v, want the default", got)
	}
}

func TestRejectedRetryScheduleIsAnError(t *testing.T) {
	rejected := nats.NewMsg("reply")
	rejected.Data = []byte(`{"error":{"code":400,"err_code":10188,"description":"message schedules is disabled"}}`)
	err := pubAckError(rejected)
	if err == nil || !strings.Contains(err.Error(), "10188") {
		t.Fatalf("rejected schedule error = %v", err)
	}

	accepted := nats.NewMsg("reply")
	accepted.Data = []byte(`{"stream":"TWITCH_INGRESS_RETRY","seq":12}`)
	if err := pubAckError(accepted); err != nil {
		t.Fatalf("accepted schedule reported %v", err)
	}
	if pubAckError(nil) == nil {
		t.Fatal("a missing acknowledgement was accepted")
	}
}

func TestAMarkedRetryIsDroppedWithACounter(t *testing.T) {
	sub := testFlowSubscriber()
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	wire.Header.Set(RetryCountHeader, "1")
	msg := mustFlowMessage(t, wire)

	// The budget check runs before anything touches the connection.
	sub.scheduleRetry(flowDelivery{wire: wire, msg: msg})
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want one drop", sub.dropped.Load(), sub.retried.Load())
	}
}

func TestAckIsReceiptLevelAndCostsNothing(t *testing.T) {
	sub := testFlowSubscriber()
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	msg := mustFlowMessage(t, wire)

	sub.pending.Add(1)
	go sub.awaitResult(flowDelivery{wire: wire, msg: msg})
	msg.Ack()

	sub.pending.Wait()
	if sub.retried.Load() != 0 || sub.dropped.Load() != 0 {
		t.Fatalf("a receipt-level ack produced traffic: %d retries, %d drops",
			sub.retried.Load(), sub.dropped.Load())
	}
}

func TestStatusHandlingNeverWaitsOnTheDataPath(t *testing.T) {
	sub := testFlowSubscriber()
	// One slot, no pump: the data path is as blocked as a saturated routine pool.
	sub.queue = make(chan flowDelivery, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 4; i++ {
			sub.deliveryCallback(laneDelivery("logical-id", []byte(`{"text":"hello"}`)))
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the delivery callback blocked behind the data path")
	}
	if sub.overrun.Load() != 3 {
		t.Fatalf("overrun counter = %d, want the three refused deliveries", sub.overrun.Load())
	}
}

func TestHeartbeatSilenceIsWhatDetectsADeletedConsumer(t *testing.T) {
	now := time.Now()
	if heartbeatLost(now, now.Add(-2*time.Second).UnixNano()) {
		t.Fatal("a live one-second heartbeat was treated as silence")
	}
	if !heartbeatLost(now, now.Add(-4*time.Second).UnixNano()) {
		t.Fatal("three missed heartbeats did not trigger recovery")
	}
}

func TestStatusesOtherThanControlNeverReprovision(t *testing.T) {
	sub := testFlowSubscriber()
	// 2.14.3 publishes nothing but 100-class control messages to a push delivery
	// subject, so a 409 here is noise and must not destroy the binding.
	deleted := flowStatus("Consumer Deleted")
	deleted.Header.Set(statusHeader, "409")
	sub.handleStatus(deleted, "409")

	if sub.recovering.Load() {
		t.Fatal("a 409 status triggered a re-provision")
	}
}

func TestDeliveryGapIsBoundedByTheAckWindow(t *testing.T) {
	// Everything inside MaxAckPending is legitimately in flight.
	if deliveriesWereLost(flowMaxAckPending, 0) {
		t.Fatal("a full in-flight window was reported as loss")
	}
	if !deliveriesWereLost(flowMaxAckPending+1, 0) {
		t.Fatal("deliveries past the ack window were not reported as lost")
	}
}

func TestFlowSubscriberRejectsForeignSubjects(t *testing.T) {
	sub := testFlowSubscriber()
	if _, err := sub.Subscribe(context.Background(), "twitch.ingress.event.premium"); err == nil {
		t.Fatal("flow subscriber accepted a subject it is not bound to")
	}
}

func TestEveryUnitSharesOnePodLaneChannel(t *testing.T) {
	sub := testFlowSubscriber()
	first, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	// One consumer per pod means one delivery stream per pod: the units compete
	// for the same channel instead of each binding its own consumer.
	if first != second {
		t.Fatal("a second consumer unit was handed its own lane channel")
	}
}

func TestFlowConsumptionIsOptOut(t *testing.T) {
	t.Setenv("NATS_CONSUME_FLOW", "")
	if !FlowConsumeEnabled() {
		t.Fatal("flow consumption is not the default")
	}
	t.Setenv("NATS_CONSUME_FLOW", "off")
	if FlowConsumeEnabled() {
		t.Fatal("NATS_CONSUME_FLOW=off did not fall back to explicit acks")
	}
}

func TestFlowConsumerScopeGuardDeclinesControlLanes(t *testing.T) {
	subscriber := &fleetSubscriber{group: "worker"}
	hot := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.event.premium"}
	control := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.status.authz.revoked"}

	if !subscriber.usesFlowConsumer(hot) {
		t.Fatal("the hot premium lane was refused a flow consumer")
	}
	if subscriber.usesFlowConsumer(control) {
		t.Fatal("a status lane was given a flow consumer")
	}

	t.Setenv("NATS_CONSUME_FLOW", "off")
	if subscriber.usesFlowConsumer(hot) {
		t.Fatal("NATS_CONSUME_FLOW=off still bound a flow consumer")
	}
}

func flowStatus(description string) *nats.Msg {
	control := nats.NewMsg("_INBOX.BAGEL.worker_premium")
	control.Header.Set(statusHeader, controlStatus)
	control.Header.Set(descriptionHeader, description)
	return control
}

func laneDelivery(id string, payload []byte) *nats.Msg {
	wire := nats.NewMsg("twitch.ingress.event.standard")
	wire.Header.Set(MessageIDHeader, id)
	wire.Data = payload
	return wire
}

func mustFlowMessage(t *testing.T, wire *nats.Msg) *Message {
	t.Helper()
	msg, err := messageFromNATS(wire)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func testFlowSubscriber() *flowSubscriber {
	return &flowSubscriber{
		stream: TwitchIngressStream.Name, subject: "twitch.ingress.event.standard",
		group: "worker", consumer: "worker_twitch_ingress_event_standard_pod_1",
		log:   zap.NewNop(),
		queue: make(chan flowDelivery, flowMaxAckPending),
		// Unbuffered, like the real lane: nothing reads it in these tests.
		output:  make(chan *Message),
		closeCh: make(chan struct{}),
	}
}
