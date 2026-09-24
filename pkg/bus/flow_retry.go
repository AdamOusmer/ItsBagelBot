// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/env"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

const RetryCountHeader = "Bagelbot-Retry"

const (
	flowRetryTimeout = 5 * time.Second

	retryLanePrefix = "twitch.ingress.retry."

	// Must stay within TwitchIngressStream.MaxAge.
	retryScheduleTTL = "5s"

	defaultFlowRetryDelay = time.Second
)

func RetryLaneSubject(lane string) string {
	return retryLanePrefix + subjectLeaf(lane)
}

func subjectLeaf(subject string) string {
	if cut := strings.LastIndex(subject, "."); cut >= 0 {
		return subject[cut+1:]
	}
	return subject
}

func (s *flowSubscriber) scheduleRetry(delivery flowDelivery) {
	if err := scheduleLaneRetry(s.nc, s.subject, delivery.wire, delivery.msg); err != nil {
		s.dropRetry(delivery.msg, err)
		return
	}
	s.retried.Add(1)
}

func scheduleLaneRetry(nc *nats.Conn, lane string, wire *nats.Msg, msg *Message) error {
	if attempt := msg.Metadata.Get(RetryCountHeader); attempt != "" {
		return fmt.Errorf("one-hop retry budget exhausted at attempt %s", attempt)
	}
	ack, err := nc.RequestMsg(retryScheduleMsg(lane, wire, flowRetryDelay(), time.Now()), flowRetryTimeout)
	if err != nil {
		return err
	}
	return pubAckError(ack)
}

// Unique subject per row: a schedule rolls up its subject and would purge an earlier retry.
func retryScheduleMsg(lane string, wire *nats.Msg, delay time.Duration, now time.Time) *nats.Msg {
	target := RetryLaneSubject(lane)
	schedule := nats.NewMsg(target + "." + nextNUID())
	schedule.Data = append([]byte(nil), wire.Data...)
	copyApplicationHeaders(schedule.Header, wire.Header)

	if wire.Header.Get(MessageIDHeader) == "" {
		schedule.Header.Set(MessageIDHeader, messageIdentity(wire))
	}

	schedule.Header.Set(jsapi.ScheduleHeader, "@at "+now.Add(delay).UTC().Format(time.RFC3339))
	schedule.Header.Set(jsapi.ScheduleTargetHeader, target)
	schedule.Header.Set(jsapi.ScheduleTTLHeader, retryScheduleTTL)
	schedule.Header.Set(RetryCountHeader, "1")
	return schedule
}

func copyApplicationHeaders(into, from nats.Header) {
	for key, values := range from {
		if len(values) == 0 || strings.HasPrefix(key, "Nats-") {
			continue
		}
		into.Set(key, values[0])
	}
}

func flowRetryDelay() time.Duration {
	delay := env.GetDuration("NATS_FLOW_RETRY_DELAY", defaultFlowRetryDelay)
	if delay <= 0 {
		return defaultFlowRetryDelay
	}
	return delay
}

func pubAckError(ack *nats.Msg) error {
	if ack == nil {
		return errors.New("bus: retry schedule got no publish acknowledgement")
	}
	var response struct {
		Error *struct {
			ErrCode     int    `json:"err_code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := codec.Unmarshal(ack.Data, &response); err != nil {
		return fmt.Errorf("bus: unreadable retry publish acknowledgement: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("bus: retry schedule rejected (%d): %s",
			response.Error.ErrCode, response.Error.Description)
	}
	return nil
}

func (s *flowSubscriber) dropRetry(msg *Message, cause error) {
	s.log.Warn("dropping failed lane event",
		zap.String("subject", s.subject),
		zap.String("message_id", msg.UUID),
		zap.Int64("dropped_total", s.dropped.Add(1)),
		zap.Error(cause))
}
