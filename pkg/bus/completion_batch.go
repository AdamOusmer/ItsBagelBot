// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"fmt"
	"sync"
	"time"
)

type CompletionBatch struct {
	mu         sync.Mutex
	subscriber *pullSubscriber
	head       *DeliveryTicket
	retire     func()
	closed     bool
	err        error
	empty      func()
}

func (c *Completion) BatchOwner() any {
	if c.subscriber == nil {
		return nil
	}
	return c.subscriber
}

func (c *Completion) NewBatch(retire, empty func()) *CompletionBatch {
	return &CompletionBatch{subscriber: c.subscriber, retire: retire, empty: empty}
}

func (c *Completion) Compact(batch *CompletionBatch) bool {
	if c.subscriber == nil {
		return false
	}
	batch.mu.Lock()
	defer batch.mu.Unlock()
	if batch.closed {
		c.Complete(batch.err)
		batch.retire()
		return true
	}
	s := c.subscriber
	ticket := c.ticket
	shard := s.pendingShard(ticket.reply)
	shard.Lock()
	defer shard.Unlock()
	if shard.deliveries[ticket.reply] != ticket {
		return false
	}
	c.mu.Lock()
	if c.state != messagePending {
		c.mu.Unlock()
		return false
	}
	c.reject, c.onResolve, c.batch = nil, nil, batch
	ticket.completion = nil
	if c.ack != nil || c.nack != nil {
		ticket.completion = c
	}
	c.mu.Unlock()
	ticket.batch, ticket.next = batch, batch.head
	if batch.head != nil {
		batch.head.previous = ticket
	}
	batch.head = ticket
	return true
}

func (ticket *DeliveryTicket) retire() {
	if ticket.batch == nil {
		ticket.completion.detach()
		return
	}
	ticket.batch.remove(ticket, messageExpired)
}

func (b *CompletionBatch) remove(ticket *DeliveryTicket, outcome messageState) *Completion {
	b.mu.Lock()
	if ticket.previous == nil && b.head != ticket {
		b.mu.Unlock()
		return nil
	}
	if ticket.previous == nil {
		b.head = ticket.next
	} else {
		ticket.previous.next = ticket.next
	}
	if ticket.next != nil {
		ticket.next.previous = ticket.previous
	}
	observer := ticket.completion
	ticket.outcome = outcome
	ticket.previous, ticket.next, ticket.completion = nil, nil, nil
	empty := b.head == nil && !b.closed
	b.mu.Unlock()
	b.subscriber.finishDelivery()
	b.retire()
	if empty {
		b.empty()
	}
	return observer
}

func (b *CompletionBatch) Complete(err error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed, b.err = true, err
	b.mu.Unlock()
	for ticket := b.first(); ticket != nil; ticket = b.first() {
		b.finish(ticket, err)
	}
}

func (b *CompletionBatch) first() *DeliveryTicket {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.head
}

func (b *CompletionBatch) Empty() bool {
	return b.first() == nil
}

func (b *CompletionBatch) finish(ticket *DeliveryTicket, err error) {
	payload := []byte("+ACK")
	outcome := messageAcked
	if err != nil {
		payload = delayedNak(max(retryDelay(ticket.retries), requestedRetryDelay(err)))
		outcome = messageNacked
	}
	observer, completed := b.completeTicket(ticket, payload, outcome)
	if observer != nil && completed {
		observer.Complete(err)
	}
}

func (b *CompletionBatch) resolveTicket(ticket *DeliveryTicket, acked bool, delay time.Duration) {
	payload := []byte("+ACK")
	outcome := messageAcked
	if !acked {
		payload = delayedNak(max(retryDelay(ticket.retries), delay))
		outcome = messageNacked
	}
	b.completeTicket(ticket, payload, outcome)
}

func (b *CompletionBatch) completeTicket(ticket *DeliveryTicket, payload []byte, outcome messageState) (*Completion, bool) {
	completed := b.publish(ticket, payload)
	if !completed {
		outcome = messageExpired
	}
	return b.remove(ticket, outcome), completed
}

func (b *CompletionBatch) publish(ticket *DeliveryTicket, payload []byte) bool {
	s := b.subscriber
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if !s.releaseDelivery(ticket.reply) {
		return false
	}
	s.progressWarning(s.nc.Publish(s.ackSubject(ticket.reply), payload))
	return true
}

func (b *CompletionBatch) observe(c *Completion) {
	b.mu.Lock()
	outcome, err := c.ticket.outcome, b.err
	if outcome == messagePending {
		c.ticket.completion = c
	}
	b.mu.Unlock()
	if outcome == messageAcked || outcome == messageNacked {
		c.Complete(err)
	}
}

func delayedNak(delay time.Duration) []byte {
	return []byte(fmt.Sprintf("-NAK {\"delay\":%d}", delay.Nanoseconds()))
}
