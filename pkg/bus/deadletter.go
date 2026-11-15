// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	DeadLetterPrefix = "dlq."

	DeadLetterSubjectHeader    = "Dlq-Subject"
	DeadLetterMsgIDHeader      = "Dlq-Msg-Id"
	DeadLetterStreamHeader     = "Dlq-Stream"
	DeadLetterConsumerHeader   = "Dlq-Consumer"
	DeadLetterDeliveriesHeader = "Dlq-Deliveries"
	DeadLetterReasonHeader     = "Dlq-Reason"

	deadLetterMaxDeliveries deadLetterReason = "max_deliveries"
	deadLetterMalformed     deadLetterReason = "malformed"

	deadLetterPublishTimeout = 2 * time.Second
	deadLetterRetryDelay     = 5 * time.Second
)

type deadLetterReason string

func deadLetterSubject(subject string) string {
	return DeadLetterPrefix + subject
}

func deadLettersStream(stream string) bool {
	return stream == DataStream.Name
}

func (s *concurrentDurableSubscriber) terminate(msg *nats.Msg, reason deadLetterReason) {
	if err := s.handoffDeadLetter(msg, reason); err != nil {
		s.retryDeadLetter(msg, err)
		return
	}
	if reason == deadLetterMaxDeliveries {
		s.logMaxDeliveries(msg)
	}
	if err := msg.Term(); err != nil {
		s.log.Warn("durable message TERM failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) transferDeadLetter(msg *nats.Msg, reason deadLetterReason) {
	s.acks.Add(1)
	go func() {
		defer s.acks.Done()
		s.terminate(msg, reason)
	}()
}

func (s *concurrentDurableSubscriber) handoffDeadLetter(msg *nats.Msg, reason deadLetterReason) error {
	if !deadLettersStream(s.stream) {
		return nil
	}
	return s.deadLetter(msg, reason)
}

func (s *concurrentDurableSubscriber) logMaxDeliveries(msg *nats.Msg) {
	var deliveries uint64
	if meta, err := msg.Metadata(); err == nil {
		deliveries = meta.NumDelivered
	}
	s.log.Warn("message terminated after max deliveries",
		zap.String("subject", msg.Subject), zap.String("stream", s.stream),
		zap.String("consumer", s.consumer), zap.Uint64("deliveries", deliveries))
}

func (s *concurrentDurableSubscriber) retryDeadLetter(msg *nats.Msg, publishErr error) {
	s.log.Error("dead letter publish failed; retaining source delivery", zap.String("subject", msg.Subject), zap.Error(publishErr))
	s.logDeadLetterExpiry(msg)
	if err := msg.NakWithDelay(deadLetterRetryDelay); err != nil {
		s.log.Warn("dead letter retry NAK failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) logDeadLetterExpiry(msg *nats.Msg) {
	storedAt := jetStreamStoredAt(msg.Reply)
	if storedAt.IsZero() || time.Since(storedAt)+deadLetterRetryDelay+deadLetterPublishTimeout < DataStream.MaxAge {
		return
	}
	s.log.Error("source message approaching DATA MaxAge without a dead letter",
		zap.String("subject", msg.Subject), zap.String("consumer", s.consumer), zap.Time("stored_at", storedAt))
}

func (s *concurrentDurableSubscriber) deadLetter(msg *nats.Msg, reason deadLetterReason) error {
	meta, err := msg.Metadata()
	if err != nil {
		return err
	}
	letter, id := s.deadLetterMsg(msg, meta, reason)
	if s.js == nil {
		return nats.ErrNoResponders
	}
	if _, err := s.js.PublishMsg(letter, nats.MsgId(id), nats.AckWait(deadLetterPublishTimeout)); err != nil {
		return err
	}
	s.log.Warn("message moved to the dead letter stream",
		zap.String("subject", msg.Subject), zap.String("dead_letter_id", id), zap.String("reason", string(reason)))
	return nil
}

func (s *concurrentDurableSubscriber) deadLetterMsg(msg *nats.Msg, meta *nats.MsgMetadata, reason deadLetterReason) (*nats.Msg, string) {
	letter := nats.NewMsg(deadLetterSubject(msg.Subject))
	letter.Data = msg.Data
	copyApplicationHeaders(letter.Header, msg.Header)
	letter.Header.Set(DeadLetterSubjectHeader, msg.Subject)
	letter.Header.Set(DeadLetterMsgIDHeader, msg.Header.Get(nats.MsgIdHdr))
	letter.Header.Set(DeadLetterStreamHeader, s.stream)
	letter.Header.Set(DeadLetterConsumerHeader, s.consumer)
	letter.Header.Set(DeadLetterReasonHeader, string(reason))
	letter.Header.Set(DeadLetterDeliveriesHeader, strconv.FormatUint(meta.NumDelivered, 10))
	id := "dlq:" + s.stream + ":" + s.consumer + ":" + strconv.FormatUint(meta.Sequence.Stream, 10)
	return letter, id
}

func (s *concurrentDurableSubscriber) retryTerminalTransfer(msg *nats.Msg) bool {
	if !deadLettersStream(s.stream) || s.delay.max == 0 {
		return false
	}
	meta, err := msg.Metadata()
	if err != nil {
		return false
	}
	if meta.NumDelivered <= s.delay.max {
		return false
	}
	deferred, err := s.deferredDelivery(meta)
	if err != nil {
		s.retryDeadLetter(msg, err)
		return true
	}
	if deferred {
		return false
	}
	s.transferDeadLetter(msg, deadLetterMaxDeliveries)
	return true
}
