// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const completionSubject = "twitch.ingress.event.premium"

type completionBroker struct {
	js       nats.JetStreamContext
	sub      bus.Subscriber
	url      string
	pub      *completionPublisher
	messages <-chan *bus.Message
}

type completionPublisher struct {
	bus.Publisher
	fail      atomic.Bool
	failAll   atomic.Bool
	ambiguous bool
}

func (p *completionPublisher) PublishOwnedWithID(ctx context.Context, subject, id string, body []byte) error {
	if p.failAll.Load() && subject != "twitch.outgress.premium" {
		return errors.New("injected pre-handoff failure")
	}
	if subject != data.SubjectCommandUsed || !p.fail.Load() {
		return p.Publisher.PublishOwnedWithID(ctx, subject, id, body)
	}
	if p.ambiguous {
		if err := p.Publisher.PublishOwnedWithID(ctx, subject, id, body); err != nil {
			return err
		}
	}
	return errors.New("injected DATA PubAck failure")
}

func newCompletionBroker(t *testing.T, ackWait ...time.Duration) completionBroker {
	t.Helper()
	wait := 10 * time.Second
	if len(ackWait) > 0 {
		wait = ackWait[0]
	}
	for key, value := range map[string]string{
		"NATS_CONSUME_FLOW": "on", "NATS_CONSUME_MODE": "pull", "NATS_INGRESS_PARTITION": "on",
		"NATS_PULL_REPLICAS": "1", "NATS_PULL_CREATE_STAGGER": "0", "NATS_PULL_ACK_WAIT": wait.String(), "NATS_PULL_NAK_DELAY": "200ms",
		"NATS_JS_DOMAIN": "hub", "NATS_HUB_URL": "", "NATS_LEAF_URL": "", "NATS_HUB_PUBLISH_URL": "", "NATS_PUBLISH_WIRE": "atomic",
	} {
		t.Setenv(key, value)
	}
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true, JetStream: true, JetStreamDomain: "hub", StoreDir: t.TempDir()})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream(nats.Domain("hub"))
	require.NoError(t, err)
	for _, spec := range []bus.StreamSpec{bus.TwitchIngressPremiumStream, bus.DataStream, bus.OutgressStream} {
		if spec.Name == bus.TwitchIngressPremiumStream.Name && len(ackWait) > 1 {
			spec.MaxAge = ackWait[1]
		}
		spec.Replicas, spec.MaxBytes, spec.PlacementTags = 1, 4<<20, nil
		require.NoError(t, bus.EnsureStreams(t.Context(), s.ClientURL(), []bus.StreamSpec{spec}, zap.NewNop()))
	}
	pub, err := bus.NewPublisher(s.ClientURL(), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pub.Close()) })
	sub, err := bus.NewSubscriber(s.ClientURL(), "completion-test", zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub.Close()) })
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	messages, err := sub.Subscribe(ctx, completionSubject)
	require.NoError(t, err)
	return completionBroker{js: js, sub: sub, url: s.ClientURL(), pub: &completionPublisher{Publisher: pub}, messages: messages}
}

func (b completionBroker) receive(t *testing.T) *bus.Message {
	t.Helper()
	select {
	case msg := <-b.messages:
		require.NotNil(t, msg)
		return msg
	case <-time.After(12 * time.Second):
		t.Fatal("completion delivery timed out")
		return nil
	}
}

func (b completionBroker) publish(t *testing.T) {
	t.Helper()
	msg := envelopeMsg(t, "wire", map[string]any{"lane": "premium", "msg_id": "twitch-chat", "event_id": "twitch-event", "chatter_user_id": "999", "text": "!hello"})
	_, err := b.js.Publish(completionSubject, msg.Payload)
	require.NoError(t, err)
}

func (b completionBroker) pipeline(t *testing.T, client valkey.Client) (*Pipeline, *LoyaltyReporter) {
	t.Helper()
	reporter := NewLoyaltyReporter(b.pub, zap.NewNop())
	dedup := NewEventDedup(idempotency.NewValkeyStore(client, "completion:", nil), "completion:", time.Hour, nil)
	m := module.NewModule("awards", module.KindCore)
	m.On(chatType, func(ctx context.Context, c *module.Context, _ module.Emit) error {
		return dedup.Effect(ctx, EffectRef{Identity: EventIdentity(&c.Env), Effect: EffectEarn}, func(ctx context.Context) error {
			reporter.EarnContext(ctx, PointEarning{BroadcasterID: c.BroadcasterID, ViewerID: 999, Points: 10})
			return nil
		})
	})
	d := Deps{Proj: fakeReader{cmd: projection.Command{Name: "hello", Response: "hello", IsActive: true}, cmdFound: true}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: b.pub, Stats: reporter, Log: zap.NewNop(), Dedup: dedup}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), m.Build()), Config{CountUses: true, OutgressPremium: "twitch.outgress.premium", OutgressStandard: "twitch.outgress.standard"})
	return p, reporter
}

func awaitCompletion(t *testing.T, msg *bus.Message, ack bool) {
	t.Helper()
	want, wrong := msg.Acked(), msg.Nacked()
	if !ack {
		want, wrong = wrong, want
	}
	select {
	case <-want:
	case <-wrong:
		t.Fatal("unexpected completion outcome")
	case <-time.After(12 * time.Second):
		t.Fatal("completion timed out")
	}
}

func crashCompletionPipeline(p *Pipeline, reporter *LoyaltyReporter) {
	close(p.uses.done)
	<-p.uses.finished
	p.uses = nil
	close(reporter.done)
	<-reporter.finished
	p.stats.Close()
	p.stats = nil
	p.completions = completionSet{}
	p.Close()
}

func (b *completionBroker) handoffCrashedDelivery(t *testing.T, msg *bus.Message) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- b.sub.Close() }()
	<-msg.Context().Done()
	msg.Nack()
	require.NoError(t, <-done)
	next, err := bus.NewSubscriber(b.url, "completion-test", zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = next.Close() })
	b.messages, err = next.Subscribe(t.Context(), completionSubject)
	require.NoError(t, err)
	b.sub = next
}

func TestProcessCompletionRedeliveryWithReceipts(t *testing.T) {
	for _, dialect := range []string{"sqlite3", "mysql"} {
		for _, mode := range []string{"rejected", "ambiguous", "crash"} {
			t.Run(dialect+"/"+mode, func(t *testing.T) { testCompletionRecovery(t, dialect, mode) })
		}
	}
}

func testCompletionRecovery(t *testing.T, dialect, mode string) {
	verifier := newCompletionReceipts(t, dialect)
	b := newCompletionBroker(t)
	client := valkeytest.Spawn(t)
	p, reporter := b.pipeline(t, client)
	b.pub.ambiguous = mode == "ambiguous"
	b.pub.fail.Store(mode != "crash")
	b.pub.failAll.Store(mode == "crash")
	b.publish(t)
	first := b.receive(t)
	require.NoError(t, p.Process(first))
	if mode == "crash" {
		crashCompletionPipeline(p, reporter)
		for _, effect := range []string{"metrics", "use", EffectEarn} {
			require.NoError(t, client.Do(t.Context(), client.B().Del().Key("completion:twitch-chat:"+effect).Build()).Error())
		}
		b.handoffCrashedDelivery(t, first)
		p, reporter = b.pipeline(t, client)
	} else {
		p.Finish(expiredCompletionContext(t))
		assertPendingCompletion(t, first)
	}
	t.Cleanup(reporter.Close)
	t.Cleanup(p.Close)
	b.pub.fail.Store(false)
	b.pub.failAll.Store(false)
	if mode != "crash" {
		p.Finish(t.Context())
		awaitCompletion(t, first, true)
		b.publish(t)
	}
	retry := b.receive(t)
	require.NoError(t, p.Process(retry))
	awaitCompletion(t, retry, true)
	b.publish(t)
	duplicate := b.receive(t)
	require.NoError(t, p.Process(duplicate))
	awaitCompletion(t, duplicate, true)
	verifier.replay(t, b.js)
}

func expiredCompletionContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}
