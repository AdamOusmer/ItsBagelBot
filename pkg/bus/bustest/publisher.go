// Package bustest provides test doubles for the message bus.
package bustest

import (
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Publisher records published messages per subject for assertions.
type Publisher struct {
	mu        sync.Mutex
	published map[string][]*message.Message
}

func NewPublisher() *Publisher {
	return &Publisher{published: make(map[string][]*message.Message)}
}

func (p *Publisher) Publish(topic string, messages ...*message.Message) error {

	p.mu.Lock()
	defer p.mu.Unlock()

	p.published[topic] = append(p.published[topic], messages...)
	return nil
}

func (p *Publisher) Close() error { return nil }

// On returns every message published on subject so far.
func (p *Publisher) On(subject string) []*message.Message {

	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]*message.Message(nil), p.published[subject]...)
}
