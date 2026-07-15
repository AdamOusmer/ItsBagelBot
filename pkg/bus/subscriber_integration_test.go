package bus

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

const nativeSubscriberTestTimeout = 3 * time.Second

type nativeSubscriberFixture struct {
	stream    string
	broadcast string
	durable   string
	shutdown  string
	cancel    string
}

type nativeSubscriberIntegration struct {
	url     string
	js      nats.JetStreamContext
	fixture nativeSubscriberFixture
}

// TestNativeSubscriberIntegration exercises the owned nats.go adapter against
// the repository's opt-in NATS 2.14 broker. The ordinary suite skips it so CI
// does not need an external server.
func TestNativeSubscriberIntegration(t *testing.T) {
	integration := openNativeSubscriberIntegration(t)
	t.Run("broadcast deliver-new fans out", func(t *testing.T) {
		testNativeBroadcastFanout(t, integration)
	})
	t.Run("durable ack nak redelivery and term", func(t *testing.T) {
		testNativeDurableDelivery(t, integration)
	})
	t.Run("close drains callbacks without a race", func(t *testing.T) {
		testNativeShutdownDrain(t, integration)
	})
	t.Run("context cancellation rejects late callbacks", func(t *testing.T) {
		testNativeContextCancellation(t, integration)
	})
}

func openNativeSubscriberIntegration(t *testing.T) nativeSubscriberIntegration {
	t.Helper()
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("NATS_INTEGRATION_URL is not set")
	}
	token := nuid.Next()
	fixture := nativeSubscriberFixture{
		stream:    "BUS_NATIVE_SUBSCRIBER_TEST_" + token,
		broadcast: "bus.native." + token + ".broadcast",
		durable:   "bus.native." + token + ".durable",
		shutdown:  "bus.native." + token + ".shutdown",
		cancel:    "bus.native." + token + ".cancel",
	}
	t.Setenv("NATS_JS_DOMAIN", "hub")
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_HUB_URL", "")

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	js, err := nc.JetStream(nats.Domain("hub"), nats.MaxWait(2*time.Second))
	if err != nil {
		nc.Close()
		t.Fatal(err)
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name: fixture.stream, Subjects: []string{"bus.native." + token + ".>"},
		Storage: nats.MemoryStorage, Replicas: 1, MaxAge: time.Minute,
	})
	if err != nil {
		nc.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = js.DeleteStream(fixture.stream)
		nc.Close()
	})
	return nativeSubscriberIntegration{url: url, js: js, fixture: fixture}
}

func testNativeBroadcastFanout(t *testing.T, integration nativeSubscriberIntegration) {
	t.Helper()
	fixture := integration.fixture
	first := openNativeBroadcastIntegrationSubscriber(t, integration.url, "broadcast-first", fixture.stream)
	second := openNativeBroadcastIntegrationSubscriber(t, integration.url, "broadcast-second", fixture.stream)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	firstMessages, err := first.Subscribe(ctx, fixture.broadcast)
	if err != nil {
		t.Fatal(err)
	}
	secondMessages, err := second.Subscribe(ctx, fixture.broadcast)
	if err != nil {
		t.Fatal(err)
	}

	wire := nats.NewMsg(fixture.broadcast)
	wire.Data = []byte("fanout")
	wire.Header.Set(MessageIDHeader, "broadcast-id")
	if _, err := integration.js.PublishMsg(wire); err != nil {
		t.Fatal(err)
	}

	for i, messages := range []<-chan *Message{firstMessages, secondMessages} {
		msg := receiveIntegrationMessage(t, messages)
		if msg.UUID != "broadcast-id" || string(msg.Payload) != "fanout" {
			t.Fatalf("subscriber %d received id=%q payload=%q", i, msg.UUID, msg.Payload)
		}
		if !msg.Ack() {
			t.Fatalf("subscriber %d could not ack", i)
		}
	}
}

func openNativeBroadcastIntegrationSubscriber(t *testing.T, url, name, stream string) Subscriber {
	t.Helper()
	nc, err := nats.Connect(url, busOptions(name)...)
	if err != nil {
		t.Fatal(err)
	}
	js, err := nc.JetStream(nats.Domain("hub"))
	if err != nil {
		nc.Close()
		t.Fatal(err)
	}
	sub := newConcurrentDurableSubscriber(concurrentSubscriberConfig{
		nc: nc, js: js, stream: stream, log: zap.NewNop(),
	})
	t.Cleanup(func() {
		if err := sub.Close(); err != nil {
			t.Errorf("close broadcast subscriber: %v", err)
		}
	})
	return sub
}

type durableIntegrationScenario struct {
	t        *testing.T
	js       nats.JetStreamContext
	stream   string
	subject  string
	consumer string
	messages <-chan *Message
	nakDelay time.Duration
}

type laneIntegrationConfig struct {
	subject  string
	group    string
	nakDelay time.Duration
}

type laneIntegrationSubscription struct {
	subscriber Subscriber
	messages   <-chan *Message
	cancel     context.CancelFunc
}

func testNativeDurableDelivery(t *testing.T, integration nativeSubscriberIntegration) {
	t.Helper()
	const group = "native-durable-test"
	const nakDelay = 50 * time.Millisecond
	fixture := integration.fixture
	lane := openNativeLaneSubscription(t, integration, laneIntegrationConfig{
		subject: fixture.durable, group: group, nakDelay: nakDelay,
	})
	scenario := durableIntegrationScenario{
		t: t, js: integration.js, stream: fixture.stream, subject: fixture.durable,
		consumer: durableName(group, fixture.durable), messages: lane.messages, nakDelay: nakDelay,
	}
	scenario.verifyDelayedRedelivery()
	scenario.verifyMalformedDeliveryTerminates()
	scenario.verifyRedeliveryBudgetTerminates()
}

func openNativeLaneSubscription(
	t *testing.T,
	integration nativeSubscriberIntegration,
	cfg laneIntegrationConfig,
) laneIntegrationSubscription {
	t.Helper()
	sub, err := NewLaneSubscriber(LaneConfig{
		URL: integration.url, Stream: integration.fixture.stream, Subject: cfg.subject,
		Group: cfg.group, NakDelay: cfg.nakDelay, MaxRedeliveries: 1,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	messages, err := sub.Subscribe(ctx, cfg.subject)
	if err != nil {
		cancel()
		_ = sub.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		if err := sub.Close(); err != nil {
			t.Errorf("close lane subscriber: %v", err)
		}
	})
	return laneIntegrationSubscription{subscriber: sub, messages: messages, cancel: cancel}
}

func (s durableIntegrationScenario) verifyDelayedRedelivery() {
	// No fleet identity header: the stream sequence fallback must stay stable
	// across a delayed redelivery.
	if _, err := s.js.Publish(s.subject, []byte("retry-then-ack")); err != nil {
		s.t.Fatal(err)
	}
	first := receiveIntegrationMessage(s.t, s.messages)
	if !strings.HasPrefix(first.UUID, "js:") {
		s.t.Fatalf("sequence fallback id = %q", first.UUID)
	}
	if !first.Nack() {
		s.t.Fatal("first delivery could not nack")
	}
	select {
	case msg := <-s.messages:
		s.t.Fatalf("redelivered before %v delay: %q", s.nakDelay, msg.UUID)
	case <-time.After(s.nakDelay / 2):
	}
	redelivered := receiveIntegrationMessage(s.t, s.messages)
	if redelivered.UUID != first.UUID {
		s.t.Fatalf("redelivery id = %q, want stable %q", redelivered.UUID, first.UUID)
	}
	if !redelivered.Ack() {
		s.t.Fatal("redelivery could not ack")
	}
	s.waitForAckPending(0)
}

func (s durableIntegrationScenario) verifyMalformedDeliveryTerminates() {
	// Malformed transport metadata is not actionable by an application handler.
	// The adapter must TERM it once rather than poison-looping until retention.
	malformed := nats.NewMsg(s.subject)
	malformed.Header["Traceparent"] = []string{"one", "two"}
	malformedAck, err := s.js.PublishMsg(malformed)
	if err != nil {
		s.t.Fatal(err)
	}
	s.waitForSequenceSettled(malformedAck.Sequence)
	s.assertNoDelivery(2*s.nakDelay, "malformed delivery escaped to handler")
}

func (s durableIntegrationScenario) verifyRedeliveryBudgetTerminates() {
	// The second failed attempt exhausts MaxRedeliveries and emits TERM. It must
	// not loop back for a third delivery.
	wire := nats.NewMsg(s.subject)
	wire.Data = []byte("term-after-budget")
	wire.Header.Set(MessageIDHeader, "term-id")
	if _, err := s.js.PublishMsg(wire); err != nil {
		s.t.Fatal(err)
	}
	if !receiveIntegrationMessage(s.t, s.messages).Nack() {
		s.t.Fatal("term message first delivery could not nack")
	}
	if !receiveIntegrationMessage(s.t, s.messages).Nack() {
		s.t.Fatal("term message final delivery could not nack")
	}
	s.waitForAckPending(0)
	s.assertNoDelivery(2*s.nakDelay, "TERMed message was delivered again")
}

func (s durableIntegrationScenario) assertNoDelivery(wait time.Duration, failure string) {
	s.t.Helper()
	select {
	case msg := <-s.messages:
		s.t.Fatalf("%s: %q", failure, msg.UUID)
	case <-time.After(wait):
	}
}

func testNativeShutdownDrain(t *testing.T, integration nativeSubscriberIntegration) {
	t.Helper()
	fixture := integration.fixture
	lane := openNativeLaneSubscription(t, integration, laneIntegrationConfig{
		subject: fixture.shutdown, group: "native-shutdown-test", nakDelay: time.Millisecond,
	})
	processed, started, readerDone := startSlowIntegrationReader(lane.messages)
	publishIntegrationBurst(t, integration.js, fixture.shutdown, 128)
	waitForIntegrationSignal(t, started, "shutdown subscriber never started")
	if err := lane.subscriber.Close(); err != nil {
		t.Fatal(err)
	}
	waitForIntegrationSignal(t, readerDone, "subscriber output did not close after drain")
	if processed.Load() == 0 {
		t.Fatal("drain abandoned every queued callback")
	}
}

func startSlowIntegrationReader(messages <-chan *Message) (*atomic.Int64, <-chan struct{}, <-chan struct{}) {
	processed := &atomic.Int64{}
	started := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		var once sync.Once
		for msg := range messages {
			once.Do(func() { close(started) })
			time.Sleep(100 * time.Microsecond)
			msg.Ack()
			processed.Add(1)
		}
	}()
	return processed, started, readerDone
}

func testNativeContextCancellation(t *testing.T, integration nativeSubscriberIntegration) {
	t.Helper()
	fixture := integration.fixture
	lane := openNativeLaneSubscription(t, integration, laneIntegrationConfig{
		subject: fixture.cancel, group: "native-context-cancel-test", nakDelay: time.Millisecond,
	})
	publishIntegrationBurst(t, integration.js, fixture.cancel, 512)

	// No reader exists while the burst arrives, leaving the serial NATS callback
	// blocked on output. Cancellation must stop that callback, reject any callback
	// nats.go starts after Unsubscribe, and close output without an Add/Wait race.
	lane.cancel()
	closed := drainIntegrationMessages(lane.messages)
	waitForIntegrationSignal(t, closed, "subscriber output did not close after context cancellation")
}

func drainIntegrationMessages(messages <-chan *Message) <-chan struct{} {
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for msg := range messages {
			msg.Ack()
		}
	}()
	return closed
}

func waitForIntegrationSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(nativeSubscriberTestTimeout):
		t.Fatal(failure)
	}
}

func publishIntegrationBurst(t *testing.T, js nats.JetStreamContext, subject string, messages int) {
	t.Helper()
	for i := 0; i < messages; i++ {
		if _, err := js.Publish(subject, []byte("cancel")); err != nil {
			t.Fatal(err)
		}
	}
}

func receiveIntegrationMessage(t *testing.T, messages <-chan *Message) *Message {
	t.Helper()
	select {
	case msg, ok := <-messages:
		if !ok {
			t.Fatal("subscriber channel closed")
		}
		return msg
	case <-time.After(nativeSubscriberTestTimeout):
		t.Fatal("timed out waiting for subscriber delivery")
		return nil
	}
}

func (s durableIntegrationScenario) waitForAckPending(want int) {
	s.t.Helper()
	info, reached := s.waitForConsumer(func(info *nats.ConsumerInfo) bool {
		return info.NumAckPending == want
	})
	if reached {
		return
	}
	s.t.Fatalf("consumer ack pending = %d, want %d", info.NumAckPending, want)
}

func (s durableIntegrationScenario) waitForSequenceSettled(wantSequence uint64) {
	s.t.Helper()
	info, reached := s.waitForConsumer(func(info *nats.ConsumerInfo) bool {
		return info.Delivered.Stream >= wantSequence && info.NumAckPending == 0
	})
	if reached {
		return
	}
	s.t.Fatalf(
		"consumer delivered stream sequence %d with %d ack pending; want sequence >= %d and 0 ack pending",
		info.Delivered.Stream,
		info.NumAckPending,
		wantSequence,
	)
}

func (s durableIntegrationScenario) waitForConsumer(
	reached func(*nats.ConsumerInfo) bool,
) (*nats.ConsumerInfo, bool) {
	s.t.Helper()
	deadline := time.Now().Add(nativeSubscriberTestTimeout)
	for time.Now().Before(deadline) {
		info, err := s.js.ConsumerInfo(s.stream, s.consumer)
		if err == nil && reached(info) {
			return info, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	info, err := s.js.ConsumerInfo(s.stream, s.consumer)
	if err != nil {
		s.t.Fatal(err)
	}
	return info, false
}
