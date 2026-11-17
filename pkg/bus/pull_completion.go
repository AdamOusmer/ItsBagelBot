// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

func (s *pullSubscriber) resolveDelivery(reply string, c *Completion, retryDelay time.Duration, acked bool) {
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if !s.releaseDelivery(reply) {
		return
	}
	payload := []byte("+ACK")
	if !acked {
		c.mu.Lock()
		delay := max(retryDelay, c.retryAfter)
		c.mu.Unlock()
		payload = []byte(fmt.Sprintf("-NAK {\"delay\":%d}", delay.Nanoseconds()))
	}
	if err := s.nc.Publish(reply, payload); err != nil {
		s.log.Warn("pull completion acknowledgement failed", zap.String("subject", s.subject), zap.Error(err))
	}
}

func (s *pullSubscriber) rejectDelivery(metadata *jsapi.MsgMetadata, msg *Message) error {
	wire := &nats.Msg{Subject: s.subject, Data: msg.Payload, Header: nats.Header{}}
	for key, value := range msg.Metadata {
		wire.Header.Set(key, value)
	}
	wire.Header.Set(MessageIDHeader, msg.UUID)
	err := s.publishPullDeadLetter(wire, metadata)
	if err != nil {
		s.log.Error("dead letter publish failed; retaining source delivery", zap.Error(err))
	}
	return err
}

func (s *pullSubscriber) publishPullDeadLetter(wire *nats.Msg, metadata *jsapi.MsgMetadata) error {
	if metadata == nil {
		return nats.ErrMsgNoReply
	}
	meta := &nats.MsgMetadata{
		Stream: metadata.Stream, Consumer: metadata.Consumer, Domain: metadata.Domain,
		NumDelivered: metadata.NumDelivered, NumPending: metadata.NumPending, Timestamp: metadata.Timestamp,
		Sequence: nats.SequencePair{Stream: metadata.Sequence.Stream, Consumer: metadata.Sequence.Consumer},
	}
	return s.letters.publishDeadLetter(wire, meta, deadLetterMalformed)
}

func (s *pullSubscriber) quarantineWire(wire jsapi.Msg) {
	core := pullWireMessage(wire)
	metadata, _ := wire.Metadata()
	err := s.publishPullDeadLetter(core, metadata)
	releasePullEnvelope(core)
	if err != nil {
		s.log.Error("dead letter publish failed; retaining source delivery", zap.Error(err))
		s.ackMu.Lock()
		defer s.ackMu.Unlock()
		if s.releaseDelivery(wire.Reply()) {
			_ = wire.NakWithDelay(deadLetterRetryDelay)
		}
		return
	}
	s.ackMu.Lock()
	defer s.ackMu.Unlock()
	if !s.releaseDelivery(wire.Reply()) {
		return
	}
	if err := wire.Ack(); err != nil {
		s.log.Warn("dead letter source ack failed", zap.Error(err))
	}
}
