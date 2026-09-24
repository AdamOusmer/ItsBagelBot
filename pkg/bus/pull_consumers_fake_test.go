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

type pullConsumerSpy struct {
	live         *jsapi.ConsumerInfo
	createErr    error
	convertAfter int

	attempts int
	created  []jsapi.ConsumerConfig
	deletes  int
}

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
	if s.live.Config.DeliverSubject != "" {
		return nil, jsapi.ErrNotPullConsumer
	}
	return &pullConsumerHandle{info: s.live}, nil
}

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

func (s *pullConsumerSpy) racedConversionLanded() bool {
	return s.convertAfter > 0 && s.attempts >= s.convertAfter && s.live != nil
}

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
