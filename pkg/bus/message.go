// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
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

	mu             sync.Mutex
	completion     *Completion
	reject         func(*Message) error
	ctx            context.Context
	receivedAt     time.Time
	brokerTime     time.Duration
	hasBrokerTime  bool
	storedAt       time.Time
	final          bool
	quarantineOnce sync.Once
}

type Completion struct {
	reject     func(error) bool
	ack        chan struct{}
	nack       chan struct{}
	onResolve  func(acked bool)
	mu         sync.Mutex
	state      messageState
	retryAfter time.Duration
	deferred   bool
	ticket     *DeliveryTicket
	subscriber *pullSubscriber
	batch      *CompletionBatch
}

func (m *Message) StoredAt() time.Time { return m.storedAt }

func (m *Message) DeliveredAt() time.Time { return m.receivedAt }

// True when a failure here ends the message: the subscriber terminates it rather than redelivering.
func (m *Message) FinalDelivery() bool { return m.final }

func (m *Message) MarkFinalDelivery() { m.final = true }

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
	messageExpired
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
	c := m.completionState()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onResolve = onResolve
}

func (m *Message) Ack() bool {
	return m.completionState().resolve(messageAcked, 0)
}

func (m *Message) Nack() bool {
	return m.completionState().resolve(messageNacked, 0)
}

func (m *Message) nackAfter(delay time.Duration) bool {
	return m.completionState().resolve(messageNacked, delay)
}

func (m *Message) requestedRetryDelay() time.Duration {
	c := m.completionState()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.retryAfter
}

func (m *Message) completionState() *Completion {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.completion == nil {
		m.completion = &Completion{reject: m.quarantine}
	}
	return m.completion
}

func (m *Message) DeferCompletion() *Completion {
	c := m.completionState()
	c.mu.Lock()
	c.deferred = true
	c.mu.Unlock()
	return c
}

func (c *Completion) Complete(err error) bool {
	if err != nil {
		return c.fail(err)
	}
	return c.resolve(messageAcked, 0)
}

func (c *Completion) fail(err error) bool {
	c.mu.Lock()
	reject := c.reject
	c.mu.Unlock()
	if reject != nil && reject(err) {
		return true
	}
	return c.resolve(messageNacked, requestedRetryDelay(err))
}

func (c *Completion) detach() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.reject = nil
	c.mu.Unlock()
}

func (c *Completion) EndExecution(err error) bool {
	c.mu.Lock()
	reject := c.reject
	c.mu.Unlock()
	quarantined := err != nil && reject != nil && reject(err)
	if !quarantined {
		c.detach()
	}
	return quarantined
}

func (m *Message) finishSuccess() {
	if !m.completionDeferred() {
		m.completionState().Complete(nil)
	}
}

func (m *Message) completionDeferred() bool {
	c := m.completionState()
	c.mu.Lock()
	deferred := c.deferred
	c.mu.Unlock()
	return deferred
}

type poisonError struct{ error }

func Poison(err error) error { return poisonError{err} }

func (m *Message) quarantine(err error) bool {
	var poison poisonError
	if !errors.As(err, &poison) || m.reject == nil {
		return false
	}
	m.quarantineOnce.Do(func() { go m.transferPoison() })
	return true
}

func (m *Message) transferPoison() {
	if transferErr := m.reject(m); transferErr != nil {
		m.nackAfter(deadLetterRetryDelay)
		return
	}
	m.Ack()
}

func (m *Completion) resolve(target messageState, retryAfter time.Duration) bool {
	won, onResolve := m.transition(target, retryAfter)
	if onResolve != nil {
		onResolve(target == messageAcked)
	}
	return won
}

func (m *Completion) transition(target messageState, retryAfter time.Duration) (bool, func(bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != messagePending {
		return m.state == target, nil
	}
	m.state = target
	m.retryAfter = retryAfter
	m.closeSignalLocked(target)
	onResolve := m.onResolve
	m.onResolve = nil
	if m.batch != nil {
		onResolve = m.resolveBatch
	}
	return true, onResolve
}

func (c *Completion) resolveBatch(acked bool) {
	c.mu.Lock()
	delay := c.retryAfter
	c.mu.Unlock()
	c.batch.resolveTicket(c.ticket, acked, delay)
}

func (m *Completion) closeSignalLocked(target messageState) {
	signal := m.ack
	if target == messageNacked {
		signal = m.nack
	}
	if signal != nil {
		close(signal)
	}
}

func (m *Message) Acked() <-chan struct{} {
	return m.completionState().signal(messageAcked)
}

func (m *Message) Nacked() <-chan struct{} {
	return m.completionState().signal(messageNacked)
}

func (m *Completion) signal(target messageState) <-chan struct{} {
	m.mu.Lock()
	m.ensureChannelsLocked()
	signal := m.nack
	if target == messageAcked {
		signal = m.ack
	}
	batch := m.batch
	m.mu.Unlock()
	if batch != nil {
		batch.observe(m)
	}
	return signal
}

func (m *Completion) ensureChannelsLocked() {
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
