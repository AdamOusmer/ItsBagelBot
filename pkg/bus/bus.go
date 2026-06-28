package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/internal/utils"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/nats-io/nats.go"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

// The bus rides the JetStream cluster from ADR 0003: at-least-once delivery
// with explicit acks, short retention, durable queue groups per service so
// every instance of a service shares one consumer and horizontal scaling
// comes for free.
//
// Streams are never auto-provisioned by the client: watermill's AutoProvision
// names a stream after the (dotted) topic, which JetStream rejects, so it can
// only ever work for single-token topics. The fleet provisions its streams
// explicitly through EnsureStreams (see provision.go) and the publisher and
// subscriber here simply bind to those existing streams by subject.

// NewPublisher connects to NATS and returns a JetStream-backed publisher. The
// target stream must already exist (see EnsureStreams); the publisher binds to
// it by subject.
func NewPublisher(url string, log *zap.Logger) (message.Publisher, error) {

	return wmnats.NewPublisher(wmnats.PublisherConfig{
		URL:         busURL(url),
		NatsOptions: busOptions(""),
		Marshaler:   &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision: false,
			// Dialed at the leaf, so target the authoritative hub JetStream
			// domain rather than the leaf's own.
			ConnectOptions: jsDomainOption(),
		},
	}, newZapAdapter(log))
}

// NewSubscriber connects to NATS and returns a durable JetStream subscriber.
// All instances passing the same group share one durable consumer, so a
// message is processed by exactly one instance and survives restarts.
func NewSubscriber(url string, group string, log *zap.Logger) (message.Subscriber, error) {
	return newSubscriber(url, group, nil, log)
}

// NewLaneSubscriber binds to a server-owned durable work-queue consumer. The
// explicit Bind is important: a consumer created implicitly by nats.go is
// deleted when the creating pod unsubscribes, which used to erase the shared
// ACK floor during every rolling update and replay the retained stream.
//
// maxRedeliveries excludes the first delivery. NATS enforces the total on the
// consumer and the Watermill delay terminates the final failed delivery, so a
// failed command cannot come back after its budget is exhausted.
func NewLaneSubscriber(url, stream, subject, group string, delay time.Duration, maxRedeliveries uint64, log *zap.Logger) (message.Subscriber, error) {
	maxDeliveries := maxRedeliveries + 1
	consumer := durableName(group, subject)

	nc, err := nats.Connect(busURL(url), busOptions(group)...)
	if err != nil {
		return nil, err
	}

	js, err := nc.JetStream(jsDomainOption()...)
	if err != nil {
		nc.Close()
		return nil, err
	}
	if err := ensureLaneConsumer(js, stream, subject, group, consumer, int(maxDeliveries)); err != nil {
		nc.Close()
		return nil, err
	}

	sub, err := wmnats.NewSubscriberWithNatsConn(nc, wmnats.SubscriberSubscriptionConfig{
		QueueGroupPrefix: group,
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		NakDelay:         wmnats.NewMaxRetryDelay(delay, maxDeliveries),
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision:     false,
			DurablePrefix:     group,
			DurableCalculator: durableName,
			ConnectOptions:    jsDomainOption(),
			SubscribeOptions: []nats.SubOpt{
				nats.Bind(stream, consumer),
			},
		},
	}, newZapAdapter(log))
	if err != nil {
		nc.Close()
		return nil, err
	}
	return sub, nil
}

const managedConsumerMetadata = "itsbagelbot.dev/managed"

func ensureLaneConsumer(js nats.JetStreamManager, stream, subject, group, name string, maxDeliveries int) error {
	desired := laneConsumerConfig(subject, group, name, maxDeliveries)

	info, err := js.ConsumerInfo(stream, name)
	switch {
	case errors.Is(err, nats.ErrConsumerNotFound):
		if _, err := js.AddConsumer(stream, desired); err != nil {
			// AddConsumer is idempotent for the same config; a different-config
			// race is surfaced because binding to it would violate our limits.
			return fmt.Errorf("bus: create consumer %q: %w", name, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("bus: inspect consumer %q: %w", name, err)
	}

	// Preserve the server-assigned delivery subject while converging old
	// consumers to the bounded retry policy. Existing ACK state is retained.
	desired.DeliverSubject = info.Config.DeliverSubject
	if _, err := js.UpdateConsumer(stream, desired); err != nil {
		return fmt.Errorf("bus: update consumer %q: %w", name, err)
	}
	return nil
}

func laneConsumerConfig(subject, group, name string, maxDeliveries int) *nats.ConsumerConfig {
	return &nats.ConsumerConfig{
		Durable:        name,
		Name:           name,
		Description:    "ItsBagelBot bounded outgress work queue",
		DeliverPolicy:  nats.DeliverAllPolicy,
		AckPolicy:      nats.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxDeliver:     maxDeliveries,
		FilterSubject:  subject,
		ReplayPolicy:   nats.ReplayInstantPolicy,
		MaxAckPending:  1000,
		DeliverSubject: "_INBOX.BAGEL." + subjectToken(name),
		DeliverGroup:   group,
		Metadata:       map[string]string{managedConsumerMetadata: "true"},
	}
}

func newSubscriber(url string, group string, nakDelay wmnats.Delay, log *zap.Logger) (message.Subscriber, error) {

	return wmnats.NewSubscriber(wmnats.SubscriberConfig{
		URL:              busURL(url),
		NatsOptions:      busOptions(group),
		QueueGroupPrefix: group,
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		NakDelay:         nakDelay,
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision:     false,
			DurablePrefix:     group,
			DurableCalculator: durableName,
			// Dialed at the leaf, so target the authoritative hub JetStream
			// domain rather than the leaf's own.
			ConnectOptions: jsDomainOption(),
		},
	}, newZapAdapter(log))
}

// durableName derives the JetStream durable consumer name for a (group, topic)
// pair. watermill's default uses the group prefix alone, which collides the
// moment a service subscribes to more than one subject (the projector folds
// several event subjects). Qualifying the durable with the topic gives each
// subscription its own consumer, surviving restarts deterministically.
//
// An empty group means a broadcast subscriber: it keeps an empty durable so
// every instance gets an ephemeral consumer and therefore every message
// (used for cache invalidation), rather than sharing one durable.
func durableName(group, topic string) string {
	if group == "" {
		return ""
	}
	return group + "_" + subjectToken(topic)
}

// subjectToken turns a dotted subject into a token usable in a JetStream
// consumer name, which may not contain dots or wildcards.
func subjectToken(subject string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_").Replace(subject)
}

// PublishJSON marshals payload and publishes it on subject with a fresh
// UUIDv7 message ID, which JetStream also uses for deduplication. When ctx
// carries a New Relic transaction, its distributed trace headers ride along
// in the message metadata so the consumer side links to the same trace.
func PublishJSON(ctx context.Context, pub message.Publisher, subject string, payload any) error {

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	id, err := utils.NewID()
	if err != nil {
		return err
	}

	msg := message.NewMessage(id.String(), body)

	if txn := newrelic.FromContext(ctx); txn != nil {

		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)

		for key := range headers {
			msg.Metadata.Set(key, headers.Get(key))
		}
	}

	return pub.Publish(subject, msg)
}

// PublishRaw is like PublishJSON but takes already-marshaled bytes and skips
// json.Marshal, for hot-path callers that marshal once into a pooled buffer.
// It publishes body on subject with a fresh UUIDv7 message ID, which JetStream
// also uses for deduplication. When ctx carries a New Relic transaction, its
// distributed trace headers ride along in the message metadata so the consumer
// side links to the same trace.
func PublishRaw(ctx context.Context, pub message.Publisher, subject string, body []byte) error {

	id, err := utils.NewID()
	if err != nil {
		return err
	}

	msg := message.NewMessage(id.String(), body)

	if txn := newrelic.FromContext(ctx); txn != nil {

		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)

		for key := range headers {
			msg.Metadata.Set(key, headers.Get(key))
		}
	}

	return pub.Publish(subject, msg)
}
