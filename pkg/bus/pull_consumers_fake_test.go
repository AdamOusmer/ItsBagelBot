// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// Shared test doubles for the pull-consumer suite: JetStream API spies and
// message fakes every seam's tests build on.

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

// The loops knob is pure parsing, so it is testable without a broker: it
// defaults to one serial fetch loop and refuses a non-positive override like
// every other knob on this lane.
