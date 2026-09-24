// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync"
	"time"
)

const (
	MessageIDHeader = messageIDHeader
)

type Metadata map[string]string

func (m Metadata) Get(key string) string { return m[key] }

func (m Metadata) Set(key, value string) { m[key] = value }

type Message struct {
	UUID     string
	Metadata Metadata
	Payload  []byte

	ack        chan struct{}
	nack       chan struct{}
	onResolve  func(acked bool)
	mu         sync.Mutex
	state      messageState
	ctx        context.Context
	receivedAt time.Time
	storedAt   time.Time
}

func (m *Message) StoredAt() time.Time { return m.storedAt }

type messageState uint8

type messageData struct {
	id       string
	payload  []byte
	metadata Metadata
	storedAt time.Time
}

const (
	messagePending messageState = iota
	messageAcked
	messageNacked
)

func NewMessage(id string, payload []byte) *Message {
	return newMessage(messageData{id: id, payload: payload, metadata: make(Metadata)})
}

func newMessage(data messageData) *Message {
	return &Message{
		UUID: data.id, Metadata: data.metadata, Payload: data.payload,
		receivedAt: time.Now(),
		storedAt:   data.storedAt,
	}
}

// Must run before the message reaches handlers; one installed after the winning Ack/Nack never runs.
func (m *Message) setResolveHandler(onResolve func(acked bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onResolve = onResolve
}

func (m *Message) Ack() bool {
	return m.resolve(messageAcked)
}

func (m *Message) Nack() bool {
	return m.resolve(messageNacked)
}

func (m *Message) resolve(target messageState) bool {
	won, onResolve := m.transition(target)
	if onResolve != nil {
		onResolve(target == messageAcked)
	}
	return won
}

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

func (m *Message) closeSignalLocked(target messageState) {
	signal := m.ack
	if target == messageNacked {
		signal = m.nack
	}
	if signal != nil {
		close(signal)
	}
}

func (m *Message) Acked() <-chan struct{} {
	return m.signal(messageAcked)
}

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

func (m *Message) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

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

type Subscriber interface {
	Subscribe(ctx context.Context, subject string) (<-chan *Message, error)
	Close() error
}
