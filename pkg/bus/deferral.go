// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

func (s *concurrentDurableSubscriber) recordDeferral(msg *nats.Msg, delay time.Duration) error {
	if !deadLettersStream(s.stream) || delay <= 0 {
		return nil
	}
	meta, err := msg.Metadata()
	if err != nil {
		return err
	}
	if s.delay.deferral <= 0 || meta.NumDelivered != s.delay.max {
		return nil
	}
	marker := nats.NewMsg(deferralSubject(meta))
	_, err = s.js.PublishMsg(marker, nats.MsgId(marker.Subject), nats.AckWait(deadLetterPublishTimeout))
	return err
}

func (s *concurrentDurableSubscriber) deferredDelivery(meta *nats.MsgMetadata) (bool, error) {
	if s.delay.deferral <= 0 || meta.NumDelivered != s.delay.deliveries() {
		return false, nil
	}
	_, err := s.js.GetLastMsg(dataDeferralStream.Name, deferralSubject(meta))
	if errors.Is(err, nats.ErrMsgNotFound) {
		return false, nil
	}
	return err == nil, err
}

func deferralSubject(meta *nats.MsgMetadata) string {
	identity := fmt.Sprintf("%s:%s:%d:%d", meta.Stream, meta.Consumer, meta.Sequence.Stream, meta.Timestamp.UnixNano())
	return fmt.Sprintf("bus.data.deferral.%x", sha256.Sum256([]byte(identity)))
}
