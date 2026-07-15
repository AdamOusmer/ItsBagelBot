// Package bustest provides test doubles for the message bus.
package bustest

import (
	"context"
	"sync"

	"ItsBagelBot/pkg/bus"
)

// Publisher records published messages per subject for assertions.
type Publisher struct {
	mu        sync.Mutex
	published map[string][]*bus.Message
}

func NewPublisher() *Publisher {
	return &Publisher{published: make(map[string][]*bus.Message)}
}

func (p *Publisher) PublishOwned(_ context.Context, topic string, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Keep the historical message-shaped assertion surface while implementing
	// the fleet-owned byte publisher. Copy because callers may recycle buffers
	// as soon as Publish returns.
	body := append([]byte(nil), payload...)
	p.published[topic] = append(p.published[topic], bus.NewMessage("", body))
	return nil
}

func (p *Publisher) PublishOwnedWithID(ctx context.Context, topic, _ string, payload []byte) error {
	return p.PublishOwned(ctx, topic, payload)
}

func (p *Publisher) Flush(context.Context) error { return nil }
func (p *Publisher) Close() error                { return nil }

// On returns every message published on subject so far.
func (p *Publisher) On(subject string) []*bus.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*bus.Message(nil), p.published[subject]...)
}
