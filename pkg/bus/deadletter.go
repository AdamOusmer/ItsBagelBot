// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

const (
	DeadLetterPrefix = "dlq."

	DeadLetterSubjectHeader    = "Bagel-Dlq-Subject"
	DeadLetterMsgIDHeader      = "Bagel-Dlq-Msg-Id"
	DeadLetterStreamHeader     = "Bagel-Dlq-Stream"
	DeadLetterConsumerHeader   = "Bagel-Dlq-Consumer"
	DeadLetterDeliveriesHeader = "Bagel-Dlq-Deliveries"
	DeadLetterReasonHeader     = "Bagel-Dlq-Reason"

	deadLetterMaxDeliveries = "max_deliveries"
	deadLetterMalformed     = "malformed"

	deadLetterPublishTimeout = 2 * time.Second
)

func deadLetterSubject(subject string) string {
	return DeadLetterPrefix + subject
}

func deadLettersStream(stream string) bool {
	return stream == BagelDataStream.Name
}

func (s *concurrentDurableSubscriber) terminate(msg *nats.Msg, reason string) {
	if deadLettersStream(s.stream) && s.js != nil {
		s.deadLetter(msg, reason)
	}
	if err := msg.Term(); err != nil {
		s.log.Warn("durable message TERM failed", zap.String("subject", msg.Subject), zap.Error(err))
	}
}

func (s *concurrentDurableSubscriber) deadLetter(msg *nats.Msg, reason string) {
	letter, id := s.deadLetterMsg(msg, reason)
	if _, err := s.js.PublishMsg(letter, nats.MsgId(id), nats.AckWait(deadLetterPublishTimeout)); err != nil {
		s.log.Error("dead letter publish failed; the message is lost",
			zap.String("subject", msg.Subject), zap.String("reason", reason), zap.Error(err))
		return
	}
	s.log.Warn("message moved to the dead letter stream",
		zap.String("subject", msg.Subject), zap.String("dead_letter_id", id), zap.String("reason", reason))
}

func (s *concurrentDurableSubscriber) deadLetterMsg(msg *nats.Msg, reason string) (*nats.Msg, string) {
	letter := nats.NewMsg(deadLetterSubject(msg.Subject))
	letter.Data = msg.Data
	copyApplicationHeaders(letter.Header, msg.Header)
	letter.Header.Set(DeadLetterSubjectHeader, msg.Subject)
	letter.Header.Set(DeadLetterMsgIDHeader, msg.Header.Get(nats.MsgIdHdr))
	letter.Header.Set(DeadLetterStreamHeader, s.stream)
	letter.Header.Set(DeadLetterConsumerHeader, s.consumer)
	letter.Header.Set(DeadLetterReasonHeader, reason)
	id := "dlq:" + s.stream + ":" + nuid.Next()
	if meta, err := msg.Metadata(); err == nil {
		letter.Header.Set(DeadLetterDeliveriesHeader, strconv.FormatUint(meta.NumDelivered, 10))
		id = "dlq:" + s.stream + ":" + strconv.FormatUint(meta.Sequence.Stream, 10)
	}
	return letter, id
}
