package bus

import (
	"context"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// Publisher is the fleet-owned durable asynchronous publish contract. Service
// code owns payload semantics while pkg/bus owns payload lifetime, IDs, trace
// propagation, pooling, batching, PubAcks, deduplication and reconnect behavior.
type Publisher interface {
	// PublishOwned admits payload to the background publisher and takes ownership
	// of its backing bytes on success. Prefer PublishJSON or PublishRaw at call
	// sites so ownership is explicit and safe.
	PublishOwned(ctx context.Context, subject string, payload []byte) error
	// Flush waits until every message admitted before the call has a final PubAck
	// (including individual dedup reconciliation after an ambiguous batch ack).
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
	body, err := sonic.ConfigFastest.Marshal(payload)
	if err != nil {
		return err
	}
	return pub.PublishOwned(ctx, subject, body)
}

// PublishRaw copies caller-owned bytes once so they may be reused immediately
// after asynchronous admission. Use PublishOwned only when transferring a
// freshly allocated buffer intentionally.
func PublishRaw(ctx context.Context, pub Publisher, subject string, payload []byte) error {
	body := append([]byte(nil), payload...)
	return pub.PublishOwned(ctx, subject, body)
}
