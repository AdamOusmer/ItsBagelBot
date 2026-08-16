package bus

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"

	"go.uber.org/zap"
)

type publishPartitionKey struct{}

// WithPublishPartition preserves ordering for one logical aggregate while
// allowing unrelated aggregates to use separate pooled connections. A channel
// or tenant ID is a good key; callers that omit it retain stream-wide ordering.
func WithPublishPartition(ctx context.Context, partition string) context.Context {
	if partition == "" {
		return ctx
	}
	return context.WithValue(ctx, publishPartitionKey{}, partition)
}

func publishPartition(ctx context.Context) string {
	partition, _ := ctx.Value(publishPartitionKey{}).(string)
	return partition
}

// Publisher is the fleet-owned durable asynchronous publish contract. Service
// code owns payload semantics while pkg/bus owns payload lifetime, message
// identity, trace propagation, pooling, batching, PubAcks and reconnect
// behavior. Fleet publishing deliberately does not use broker deduplication.
//
// Delivery is at-least-once. The default Fast-Ingest wire stores each message
// on arrival rather than on commit and the broker never rolls a session back,
// so a caller that retries a reported failure can store a message twice. The
// publisher narrows that window as far as the wire allows — it reports the
// prefix the broker acknowledged as delivered and fails only the rest — but it
// cannot close it.
type Publisher interface {
	// PublishOwned admits payload to the background publisher and takes ownership
	// of its backing bytes on success. Prefer PublishJSON or PublishRaw at call
	// sites so ownership is explicit and safe.
	PublishOwned(ctx context.Context, subject string, payload []byte) error
	// PublishOwnedWithID publishes one logical output under a caller-supplied
	// fleet message identity and waits for its cohort's final PubAck. The ID
	// is not sent as Nats-Msg-Id and does not make replays idempotent. An error
	// means the message was not acknowledged, not that it was not stored.
	PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error
	// Flush waits until every message admitted before the call has resolved and
	// reports the first failure that overlapped that window. It is a per-call
	// result, not a latch: the failure it reports is cleared, so a later Flush
	// answers for its own window.
	Flush(ctx context.Context) error
	Close() error
}

// NewPublisher builds a pooled NATS 2.14 publisher. Connections are selected
// by StreamRouter so each stream retains deterministic ordering while separate
// streams can publish in parallel.
func NewPublisher(url string, log *zap.Logger) (Publisher, error) {
	return newPublisherPool(url, log)
}

// PublishJSON gives Sonic's result buffer directly to the asynchronous
// publisher. There is no intermediate message object or payload copy.
func PublishJSON(ctx context.Context, pub Publisher, subject string, payload any) error {
	encodeSegment := startMessagingSegment(ctx, messagingSpan{
		name: "nats.publish.encode", operation: "publish", destination: subject,
	})
	body, err := codec.FastMarshal(payload)
	endMessagingSegment(encodeSegment, err)
	if err != nil {
		return err
	}
	return publishOwned(ctx, pub, subject, body)
}

// PublishRaw copies caller-owned bytes once so they may be reused immediately
// after asynchronous admission. Use PublishOwned only when transferring a
// freshly allocated buffer intentionally.
func PublishRaw(ctx context.Context, pub Publisher, subject string, payload []byte) error {
	body := append([]byte(nil), payload...)
	return publishOwned(ctx, pub, subject, body)
}

// Publication is one caller-owned payload and its stable fleet message identity.
type Publication struct {
	Subject string
	ID      string
	Payload []byte
}

// PublishConfirmed copies caller-owned bytes, publishes them under a stable
// fleet identity and waits for the final acknowledgement. Rejecting an
// empty ID keeps subscriber message identity explicit; it does not enable NATS
// broker deduplication.
func PublishConfirmed(ctx context.Context, pub Publisher, publication Publication) error {
	if publication.ID == "" {
		return errors.New("bus: confirmed publish requires a message ID")
	}
	body := append([]byte(nil), publication.Payload...)
	segment := startMessagingSegment(ctx, messagingSpan{
		name: "nats.publish", operation: "publish", destination: publication.Subject,
	})
	err := pub.PublishOwnedWithID(ctx, publication.Subject, publication.ID, body)
	endMessagingSegment(segment, err)
	return err
}

func publishOwned(ctx context.Context, pub Publisher, subject string, body []byte) error {
	segment := startMessagingSegment(ctx, messagingSpan{
		name: "nats.publish", operation: "publish", destination: subject,
	})
	err := pub.PublishOwned(ctx, subject, body)
	endMessagingSegment(segment, err)
	return err
}
