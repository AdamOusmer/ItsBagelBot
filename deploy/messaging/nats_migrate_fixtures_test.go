// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// consumerSnapshot is the observable state of a consumer worth comparing
// before a snapshot and after a restore.
type consumerSnapshot struct {
	name       string
	ackFloor   uint64
	numPending uint64
}

type consumerRef struct {
	stream string
	name   string
}

type streamFixture struct {
	name      string
	published int
	consumer  consumerSnapshot
}

const fixtureMessages = 6
const fixtureAcked = 3

// streamFixtureSpec is the per-stream-type shape a fixture is built from:
// its retention (limits keeps everything, work-queue deletes on ack), its
// consumer's name, and the message count it settles on once fixtureAcked
// have been acked.
type streamFixtureSpec struct {
	name      string
	retention jetstream.RetentionPolicy
	consumer  string
	afterAck  int
}

type migrateSession struct {
	nc *nats.Conn
	js jetstream.JetStream
}

func seedLimitsStream(t *testing.T, nc *nats.Conn, js jetstream.JetStream, name string) streamFixture {
	t.Helper()
	spec := streamFixtureSpec{name: name, retention: jetstream.LimitsPolicy, consumer: "watcher", afterAck: fixtureMessages}
	return seedStreamFixture(t, migrateSession{nc: nc, js: js}, spec, ackViaPush)
}

func seedWorkQueueStream(t *testing.T, nc *nats.Conn, js jetstream.JetStream, name string) streamFixture {
	t.Helper()
	spec := streamFixtureSpec{name: name, retention: jetstream.WorkQueuePolicy, consumer: "worker", afterAck: fixtureMessages - fixtureAcked}
	return seedStreamFixture(t, migrateSession{nc: nc, js: js}, spec, ackViaPull)
}

// ackStrategy creates spec's consumer and acks fixtureAcked deliveries from
// it, in whatever way its consumer type requires.
type ackStrategy func(t *testing.T, sess migrateSession, spec streamFixtureSpec)

func seedStreamFixture(t *testing.T, sess migrateSession, spec streamFixtureSpec, ack ackStrategy) streamFixture {
	t.Helper()
	subject := spec.name + ".msg"
	createFixtureStream(t, sess.js, spec, subject)
	publishFixtureMessages(t, sess.js, subject, fixtureMessages)
	ack(t, sess, spec)

	ref := consumerRef{stream: spec.name, name: spec.consumer}
	consumer := awaitConsumerSnapshot(t, sess.nc, ref, fixtureAcked)
	published := awaitMessageCount(t, sess.js, spec.name, spec.afterAck)
	return streamFixture{name: spec.name, published: published, consumer: consumer}
}

func createFixtureStream(t *testing.T, js jetstream.JetStream, spec streamFixtureSpec, subject string) {
	t.Helper()
	_, err := js.CreateStream(context.Background(), jetstream.StreamConfig{
		Name: spec.name, Subjects: []string{subject}, Retention: spec.retention, Replicas: 3,
	})
	if err != nil {
		t.Fatalf("create stream %s: %v", spec.name, err)
	}
}

func ackViaPush(t *testing.T, sess migrateSession, spec streamFixtureSpec) {
	t.Helper()
	deliver := spec.name + ".deliver"
	createFixtureConsumer(t, sess.js, spec.name, jetstream.ConsumerConfig{
		Durable: spec.consumer, AckPolicy: jetstream.AckExplicitPolicy, DeliverSubject: deliver,
	})
	ackPushDeliveries(t, sess.nc, deliver, fixtureAcked)
}

func ackViaPull(t *testing.T, sess migrateSession, spec streamFixtureSpec) {
	t.Helper()
	cons := createFixtureConsumer(t, sess.js, spec.name, jetstream.ConsumerConfig{
		Durable: spec.consumer, AckPolicy: jetstream.AckExplicitPolicy,
	})
	ackPullDeliveries(t, cons, fixtureAcked)
}

func createFixtureConsumer(t *testing.T, js jetstream.JetStream, stream string, cfg jetstream.ConsumerConfig) jetstream.Consumer {
	t.Helper()
	cons, err := js.CreateOrUpdateConsumer(context.Background(), stream, cfg)
	if err != nil {
		t.Fatalf("create consumer for %s: %v", stream, err)
	}
	return cons
}

// currentMessageCount reads the stream's live count instead of assuming
// fixtureMessages: a work-queue stream removes a message as soon as its
// one consumer acks it, so the count can already be lower by fixture time.
func currentMessageCount(t *testing.T, js jetstream.JetStream, name string) int {
	t.Helper()
	ctx := context.Background()
	s, err := js.Stream(ctx, name)
	if err != nil {
		t.Fatalf("stream %s for message count: %v", name, err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatalf("stream info %s for message count: %v", name, err)
	}
	return int(info.State.Msgs)
}

// awaitMessageCount polls for the stream to settle on want: a work-queue
// stream's ack-driven delete runs on the stream's own Raft group, separate
// from the consumer's, so it can still be in flight once the ack floor and
// the fixture's synchronous calls have already returned.
func awaitMessageCount(t *testing.T, js jetstream.JetStream, name string, want int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last int
	for time.Now().Before(deadline) {
		last = currentMessageCount(t, js, name)
		if last == want {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("stream %s message count never reached %d, last seen %d", name, want, last)
	return last
}

func publishFixtureMessages(t *testing.T, js jetstream.JetStream, subject string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if _, err := js.Publish(ctx, subject, []byte(fmt.Sprintf("payload-%d", i))); err != nil {
			t.Fatalf("publish to %s: %v", subject, err)
		}
	}
}

func ackPushDeliveries(t *testing.T, nc *nats.Conn, deliver string, n int) {
	t.Helper()
	sub, err := nc.SubscribeSync(deliver)
	if err != nil {
		t.Fatalf("subscribe push deliveries on %s: %v", deliver, err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	for i := 0; i < n; i++ {
		ackOnePushDelivery(t, sub, i)
	}
}

func ackOnePushDelivery(t *testing.T, sub *nats.Subscription, index int) {
	t.Helper()
	msg, err := sub.NextMsg(3 * time.Second)
	if err != nil {
		t.Fatalf("await push delivery %d on %s: %v", index, sub.Subject, err)
	}
	if err := msg.Respond(nil); err != nil {
		t.Fatalf("ack push delivery %d on %s: %v", index, sub.Subject, err)
	}
}

func ackPullDeliveries(t *testing.T, cons jetstream.Consumer, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		fetchAndAckOne(t, cons, i)
	}
}

func fetchAndAckOne(t *testing.T, cons jetstream.Consumer, index int) {
	t.Helper()
	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(3*time.Second))
	if err != nil {
		t.Fatalf("fetch pull delivery %d: %v", index, err)
	}
	for msg := range msgs.Messages() {
		if err := msg.Ack(); err != nil {
			t.Fatalf("ack pull delivery %d: %v", index, err)
		}
	}
	if err := msgs.Error(); err != nil {
		t.Fatalf("pull delivery %d batch error: %v", index, err)
	}
}

// readConsumerSnapshot uses the legacy JetStreamManager, since the modern
// jetstream.Stream.Consumer refuses to hand back a push consumer.
func readConsumerSnapshot(t *testing.T, nc *nats.Conn, ref consumerRef) consumerSnapshot {
	t.Helper()
	jsm, err := nc.JetStream(nats.Domain("hub"))
	if err != nil {
		t.Fatalf("jetstream manager: %v", err)
	}
	info, err := jsm.ConsumerInfo(ref.stream, ref.name)
	if err != nil {
		t.Fatalf("consumer info %s/%s: %v", ref.stream, ref.name, err)
	}
	return consumerSnapshot{name: ref.name, ackFloor: info.AckFloor.Consumer, numPending: info.NumPending}
}

// awaitConsumerSnapshot polls for the ack floor to settle: acking a message
// proposes a Raft entry on a replicated consumer, so it lands slightly
// after the ack publish that triggered it.
func awaitConsumerSnapshot(t *testing.T, nc *nats.Conn, ref consumerRef, wantAckFloor uint64) consumerSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last consumerSnapshot
	for time.Now().Before(deadline) {
		last = readConsumerSnapshot(t, nc, ref)
		if last.ackFloor == wantAckFloor {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("consumer %s/%s ack floor never reached %d, last seen %+v", ref.stream, ref.name, wantAckFloor, last)
	return last
}

const kvBucket = "REHEARSAL_KV"

func seedKVBucket(t *testing.T, js jetstream.JetStream) map[string]string {
	t.Helper()
	ctx := context.Background()
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: kvBucket, History: 1, Replicas: 3})
	if err != nil {
		t.Fatalf("create kv bucket %s: %v", kvBucket, err)
	}
	values := map[string]string{"alpha": "one", "beta": "two", "gamma": "three"}
	for key, value := range values {
		if _, err := kv.Put(ctx, key, []byte(value)); err != nil {
			t.Fatalf("put kv %s=%s: %v", key, value, err)
		}
	}
	return values
}
