package bus

import (
	"context"
	"sync"
)

const (
	// MessageIDHeader carries the fleet's logical message identity without
	// enabling JetStream's broker-side deduplication index.
	MessageIDHeader = messageIDHeader

	// legacyMessageIDHeader is accepted for retained pre-migration messages and
	// temporarily dual-written so old consumers remain safe during the native
	// subscriber's rolling deployment. It is application identity, never a
	// JetStream deduplication key.
	legacyMessageIDHeader = "_watermill_message_uuid"
)

// Metadata is transport metadata copied from the NATS headers. Values are
// intentionally single-valued: every fleet publisher uses Header.Set and a
// multi-valued wire header is rejected by the subscriber as malformed.
type Metadata map[string]string

// Get returns the metadata value or an empty string when absent.
func (m Metadata) Get(key string) string { return m[key] }

// Set stores one metadata value.
func (m Metadata) Set(key, value string) { m[key] = value }

// Message is the fleet-owned delivery unit shared by every bus consumer. Ack
// and Nack are non-blocking, idempotent signals; the native NATS subscriber
// reconciles the selected result with JetStream outside its serial callback.
type Message struct {
	UUID     string
	Metadata Metadata
	Payload  []byte

	ack   chan struct{}
	nack  chan struct{}
	mu    sync.Mutex
	state messageState
	ctx   context.Context
}

type messageState uint8

const (
	messagePending messageState = iota
	messageAcked
	messageNacked
)

// NewMessage constructs a delivery with independent acknowledgement state.
func NewMessage(id string, payload []byte) *Message {
	return newMessage(id, payload, make(Metadata))
}

func newMessage(id string, payload []byte, metadata Metadata) *Message {
	return &Message{
		UUID: id, Metadata: metadata, Payload: payload,
		ack: make(chan struct{}), nack: make(chan struct{}),
	}
}

// Ack marks the message successfully handled. It returns false only when Nack
// won the acknowledgement race first.
func (m *Message) Ack() bool {
	return m.resolve(messageAcked)
}

// Nack marks the message for paced redelivery. It returns false only when Ack
// won the acknowledgement race first.
func (m *Message) Nack() bool {
	return m.resolve(messageNacked)
}

func (m *Message) resolve(target messageState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureChannelsLocked()
	if m.state != messagePending {
		return m.state == target
	}
	m.state = target
	if target == messageAcked {
		close(m.ack)
	} else {
		close(m.nack)
	}
	return true
}

// Acked is closed after the message is acknowledged.
func (m *Message) Acked() <-chan struct{} {
	return m.signal(messageAcked)
}

// Nacked is closed after the message is negatively acknowledged.
func (m *Message) Nacked() <-chan struct{} {
	return m.signal(messageNacked)
}

func (m *Message) signal(target messageState) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureChannelsLocked()
	if target == messageAcked {
		return m.ack
	}
	return m.nack
}

func (m *Message) ensureChannelsLocked() {
	m.ack = messageSignal(m.ack, m.state == messageAcked)
	m.nack = messageSignal(m.nack, m.state == messageNacked)
}

func messageSignal(signal chan struct{}, resolved bool) chan struct{} {
	if signal != nil {
		return signal
	}
	signal = make(chan struct{})
	if resolved {
		close(signal)
	}
	return signal
}

// Context returns the delivery context, defaulting to Background for messages
// constructed directly in tests or by non-subscriber code.
func (m *Message) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// SetContext attaches tracing, cancellation, and request-scoped values.
func (m *Message) SetContext(ctx context.Context) { m.ctx = ctx }

// Subscriber is the fleet-owned consuming contract. Implementations close the
// returned channel when ctx is cancelled or Close releases the subscription.
type Subscriber interface {
	Subscribe(ctx context.Context, subject string) (<-chan *Message, error)
	Close() error
}
