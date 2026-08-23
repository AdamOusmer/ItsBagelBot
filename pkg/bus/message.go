// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"time"
)

const (
	// MessageIDHeader carries the fleet's logical message identity without
	// enabling JetStream's broker-side deduplication index.
	MessageIDHeader = messageIDHeader
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
// and Nack are idempotent: the first call decides the result and every later one
// is a no-op that reports the decision.
//
// How the result reaches the broker differs by subscriber, and so does what the
// call costs. The explicit-ACK subscriber selects on Acked/Nacked from its own
// goroutine, so both calls return immediately there. The receipt-level lane
// adapters install a resolve handler instead (setResolveHandler) and run it on
// the resolving goroutine: an ACK still costs nothing, because the flow-control
// response or the batch floor already owns the ack floor, while a NACK publishes
// the retry schedule inline.
type Message struct {
	UUID     string
	Metadata Metadata
	Payload  []byte

	// ack and nack are created on first use rather than per delivery. The hot
	// lane adapters resolve through onResolve and never read either signal, so
	// eager allocation was two channels of garbage per message at lane rate.
	ack  chan struct{}
	nack chan struct{}
	// onResolve is the lane adapters' reconciliation hook. It is invoked exactly
	// once, by whichever Ack/Nack wins, and never under mu.
	onResolve func(acked bool)
	mu        sync.Mutex
	state     messageState
	ctx       context.Context
	// receivedAt is set at the native subscriber boundary. It lets the consumer
	// transaction distinguish delivery/admission wait from handler execution.
	receivedAt time.Time
}

type messageState uint8

type messageData struct {
	id       string
	payload  []byte
	metadata Metadata
}

const (
	messagePending messageState = iota
	messageAcked
	messageNacked
)

// NewMessage constructs a delivery with independent acknowledgement state.
func NewMessage(id string, payload []byte) *Message {
	return newMessage(messageData{id: id, payload: payload, metadata: make(Metadata)})
}

// newMessage deliberately leaves both acknowledgement signals nil. Nothing on
// the hot path reads them, and ensureChannelsLocked mints whichever one a caller
// asks for in the state the message is already in.
func newMessage(data messageData) *Message {
	return &Message{
		UUID: data.id, Metadata: data.metadata, Payload: data.payload,
		receivedAt: time.Now(),
	}
}

// setResolveHandler installs the callback that reconciles this delivery with its
// lane. It must be set before the message is handed to handlers: the handler is
// read at resolution time, so one installed after the winning Ack/Nack is never
// called at all.
//
// The callback runs exactly once, after the winning transition and OUTSIDE mu —
// the lane adapters publish a retry schedule from it, and holding the message
// lock across that network round trip would block every later Ack/Nack on it.
// Losing Ack/Nack calls never re-enter it.
func (m *Message) setResolveHandler(onResolve func(acked bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onResolve = onResolve
}

// Ack marks the message successfully handled. It returns false only when Nack
// won the acknowledgement race first.
//
// On the explicit-ACK subscriber this releases one JetStream delivery. On the
// hot ingress lanes' flow-controlled subscriber it is receipt-level: the ack
// floor for a whole delivery window is advanced by the flow-control responses
// the subscriber sends, not by this call, so Ack there records the handler's
// verdict rather than acknowledging one message.
func (m *Message) Ack() bool {
	return m.resolve(messageAcked)
}

// Nack marks the message for paced redelivery. It returns false only when Ack
// won the acknowledgement race first.
//
// The flow-controlled subscriber has no per-message pending state to NAK
// against, so it schedules the event onto the lane's retry subject exactly once
// instead (see RetryCountHeader); ordering is lost and a second failure drops
// the event. That schedule is published from this call, on the caller's own
// goroutine, so a Nack there blocks for as long as flowRetryTimeout allows —
// which is affordable only because failure is rare by contract on those lanes.
func (m *Message) Nack() bool {
	return m.resolve(messageNacked)
}

// resolve commits the first result and then, with the lock released, hands it to
// the lane adapter. The split is the whole point: the transition has to be
// atomic, the callback must not be.
func (m *Message) resolve(target messageState) bool {
	won, onResolve := m.transition(target)
	if onResolve != nil {
		onResolve(target == messageAcked)
	}
	return won
}

// transition is the locked half. It reports whether this call was the winning
// one and returns the resolve handler only to that winner, so a losing Ack or
// Nack can never invoke it a second time.
func (m *Message) transition(target messageState) (bool, func(bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != messagePending {
		return m.state == target, nil
	}
	m.state = target
	m.closeSignalLocked(target)
	return true, m.onResolve
}

// closeSignalLocked closes the winning signal only when someone has already
// asked for it. A nil one is not an omission: messageSignal mints it closed for
// the resolved state whenever Acked or Nacked is finally called.
func (m *Message) closeSignalLocked(target messageState) {
	signal := m.ack
	if target == messageNacked {
		signal = m.nack
	}
	if signal != nil {
		close(signal)
	}
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

func (m *Message) deliveryWait(now time.Time) time.Duration {
	if m.receivedAt.IsZero() {
		return 0
	}
	wait := now.Sub(m.receivedAt)
	if wait < 0 {
		return 0
	}
	return wait
}

// Subscriber is the fleet-owned consuming contract. Implementations close the
// returned channel when ctx is cancelled or Close releases the subscription.
type Subscriber interface {
	Subscribe(ctx context.Context, subject string) (<-chan *Message, error)
	Close() error
}
