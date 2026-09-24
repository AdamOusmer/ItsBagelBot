// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bustest

import (
	"context"
	"sync"

	"ItsBagelBot/pkg/bus"
)

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

	body := append([]byte(nil), payload...)
	p.published[topic] = append(p.published[topic], bus.NewMessage("", body))
	return nil
}

func (p *Publisher) PublishOwnedWithID(ctx context.Context, topic, _ string, payload []byte) error {
	return p.PublishOwned(ctx, topic, payload)
}

func (p *Publisher) Flush(context.Context) error { return nil }
func (p *Publisher) Close() error                { return nil }

func (p *Publisher) On(subject string) []*bus.Message {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*bus.Message(nil), p.published[subject]...)
}
