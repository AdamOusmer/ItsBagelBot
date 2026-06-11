package bus

import (
	"context"
	"encoding/json"
	"net/http"
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
// comes for free. Stream retention limits are an ops concern (kept tight on
// the broker); AutoProvision only covers the local/dev case.

// NewPublisher connects to NATS and returns a JetStream-backed publisher.
func NewPublisher(url string, log *zap.Logger) (message.Publisher, error) {

	return wmnats.NewPublisher(wmnats.PublisherConfig{
		URL:       url,
		Marshaler: &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision: true,
		},
	}, newZapAdapter(log))
}

// NewSubscriber connects to NATS and returns a durable JetStream subscriber.
// All instances passing the same group share one durable consumer, so a
// message is processed by exactly one instance and survives restarts.
func NewSubscriber(url string, group string, log *zap.Logger) (message.Subscriber, error) {

	return wmnats.NewSubscriber(wmnats.SubscriberConfig{
		URL:              url,
		QueueGroupPrefix: group,
		SubscribersCount: 1,
		AckWaitTimeout:   30 * time.Second,
		Unmarshaler:      &wmnats.NATSMarshaler{},
		JetStream: wmnats.JetStreamConfig{
			AutoProvision: true,
			DurablePrefix: group,
		},
	}, newZapAdapter(log))
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
