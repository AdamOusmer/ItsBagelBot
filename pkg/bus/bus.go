package bus

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/internal/utils"

	wmnats "github.com/ThreeDotsLabs/watermill-nats/v2/pkg/nats"
	"github.com/ThreeDotsLabs/watermill/message"

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
		URL:         url,
		NatsOptions: options(""),
		Marshaler:   &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision: false,
		},
	}, newZapAdapter(log))
}

// NewSubscriber connects to NATS and returns a durable JetStream subscriber.
// All instances passing the same group share one durable consumer, so a
// message is processed by exactly one instance and survives restarts.
func NewSubscriber(url string, group string, log *zap.Logger) (message.Subscriber, error) {
	return newSubscriber(url, group, nil, log)
}

// NewLaneSubscriber is NewSubscriber with paced redelivery: a nacked message
// comes back after delay instead of immediately, and is dropped for good
// after maxRetries redeliveries. Built for consumers that nack on rate
// limits, where instant redelivery would spin against the limiter until the
// tokens refill.
func NewLaneSubscriber(url string, group string, delay time.Duration, maxRetries uint64, log *zap.Logger) (message.Subscriber, error) {
	return newSubscriber(url, group, wmnats.NewMaxRetryDelay(delay, maxRetries), log)
}

func newSubscriber(url string, group string, nakDelay wmnats.Delay, log *zap.Logger) (message.Subscriber, error) {

	return wmnats.NewSubscriber(wmnats.SubscriberConfig{
		URL:              url,
		NatsOptions:      options(group),
		QueueGroupPrefix: group,
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		NakDelay:         nakDelay,
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision:     false,
			DurablePrefix:     group,
			DurableCalculator: durableName,
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

	msg := message.NewMessage(id, body)

	if txn := newrelic.FromContext(ctx); txn != nil {

		headers := http.Header{}
		txn.InsertDistributedTraceHeaders(headers)

		for key := range headers {
			msg.Metadata.Set(key, headers.Get(key))
		}
	}

	return pub.Publish(subject, msg)
}
