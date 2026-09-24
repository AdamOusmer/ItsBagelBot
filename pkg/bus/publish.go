// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"

	"go.uber.org/zap"
)

type publishPartitionKey struct{}

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

// On NATS_PUBLISH_WIRE=fast, retrying a reported failure can store a message twice.
type Publisher interface {
	// Takes ownership of payload's bytes on success; the caller must not reuse them.
	PublishOwned(ctx context.Context, subject string, payload []byte) error
	// An error means unacknowledged, not unstored; id does not make a retry idempotent.
	PublishOwnedWithID(ctx context.Context, subject, id string, payload []byte) error
	Flush(ctx context.Context) error
	Close() error
}

func NewPublisher(url string, log *zap.Logger) (Publisher, error) {
	return newPublisherPool(url, log)
}

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

func PublishRaw(ctx context.Context, pub Publisher, subject string, payload []byte) error {
	body := append([]byte(nil), payload...)
	return publishOwned(ctx, pub, subject, body)
}

type Publication struct {
	Subject string
	ID      string
	Payload []byte
}

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
