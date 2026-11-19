// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type DeliveryTicket struct {
	reply      string
	stored     int64
	received   int32
	brokerTime time.Duration
	touched    int32
	handed     bool
	held       bool
	executed   bool
	retries    uint8
	outcome    messageState
	async      int32
	completion *Completion
	batch      *CompletionBatch
	previous   *DeliveryTicket
	next       *DeliveryTicket
}

type pullPendingShard struct {
	sync.Mutex
	deliveries map[string]*DeliveryTicket
}

func (s *pullSubscriber) pendingShard(reply string) *pullPendingShard {
	reply = deliverySuffix(reply)
	var hash uint32
	for i := range reply {
		hash = hash*31 + uint32(reply[i])
	}
	return &s.pending[hash%uint32(len(s.pending))]
}

func deliverySuffix(reply string) string {
	if !strings.HasPrefix(reply, "$JS.ACK.") {
		return reply
	}
	end := len(reply)
	for range 5 {
		end = strings.LastIndexByte(reply[:end], '.')
	}
	return reply[end+1:]
}

func (s *pullSubscriber) ackSubject(reply string) string {
	if strings.HasPrefix(reply, "$JS.ACK.") {
		return reply
	}
	return s.replyPrefix + reply
}

func (s *pullSubscriber) deliveryTicket(reply string) *DeliveryTicket {
	shard := s.pendingShard(reply)
	shard.Lock()
	defer shard.Unlock()
	return shard.deliveries[deliverySuffix(reply)]
}

func deliveryRetryDelay(metadata *jsapi.MsgMetadata) time.Duration {
	return retryDelay(deliveryRetries(metadata))
}

func deliveryRetries(metadata *jsapi.MsgMetadata) uint8 {
	if metadata == nil || metadata.NumDelivered == 0 {
		return 0
	}
	return uint8(min(metadata.NumDelivered-1, 4))
}

func retryDelay(retries uint8) time.Duration {
	return min(pullNakDelay()*time.Duration(1<<retries), 15*time.Second)
}

func (s *pullSubscriber) trackDelivery(wire jsapi.Msg) {
	metadata, _ := wire.Metadata()
	now := time.Now()
	stored := now
	if metadata != nil {
		stored = metadata.Timestamp
	}
	reply := deliverySuffix(wire.Reply())
	s.replyOnce.Do(func() {
		s.replyPrefix = strings.Clone(wire.Reply()[:len(wire.Reply())-len(reply)])
	})
	reply = strings.Clone(reply)
	shard := s.pendingShard(reply)
	shard.Lock()
	defer shard.Unlock()
	if shard.deliveries == nil {
		shard.deliveries = make(map[string]*DeliveryTicket)
	}
	brokerTime := now.Sub(stored)
	age := int32(brokerTime.Milliseconds())
	shard.deliveries[reply] = &DeliveryTicket{reply: reply, stored: stored.UnixNano(), received: age, touched: age, brokerTime: brokerTime, retries: deliveryRetries(metadata)}
}

func (s *pullSubscriber) markHanded(ticket *DeliveryTicket) {
	if s.push == nil {
		return
	}
	shard := s.pendingShard(ticket.reply)
	shard.Lock()
	ticket.handed = true
	shard.Unlock()
}

func (s *pullSubscriber) releaseDelivery(reply string) bool {
	reply = deliverySuffix(reply)
	shard := s.pendingShard(reply)
	shard.Lock()
	delivery, exists := shard.deliveries[reply]
	delete(shard.deliveries, reply)
	shard.Unlock()
	if !exists {
		return false
	}
	if s.sourceExpired(delivery, time.Now()) || !s.deliveryHeartbeatAlive() {
		return false
	}
	if s.progressExpired(delivery, time.Now()) {
		s.expired.Add(1)
		s.reportExpired(1)
		return false
	}
	return true
}

func (s *pullSubscriber) heartbeat() {
	defer close(s.heartbeatDone)
	ticker := time.NewTicker(s.heartbeatInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.refreshPending()
			s.refreshHorizon()
		case <-s.heartbeatStop:
			return
		}
	}
}

func (s *pullSubscriber) refreshHorizon() {
	if s.hot != nil {
		s.hot.tick()
		return
	}
	s.refreshStream()
}

func (s *pullSubscriber) refreshStream() *nats.StreamInfo {
	info, err := s.letters.js.StreamInfo(s.stream, nats.MaxWait(min(s.heartbeatInterval(), time.Second)))
	if err != nil {
		s.markLaneError()
		s.progressWarning(err)
		return nil
	}
	s.horizon.Store(int64(info.Config.MaxAge))
	return info
}

func (s *pullSubscriber) refreshPending() {
	oldest := time.Now()
	for i := range s.pending {
		oldest = minTime(oldest, s.refreshShard(&s.pending[i]))
	}
	s.reportHorizon(oldest)
}

func (s *pullSubscriber) refreshShard(shard *pullPendingShard) time.Time {
	progress := s.progressReplies(shard, time.Now())
	for _, ticket := range progress.retired {
		ticket.retire()
	}
	for _, reply := range progress.replies {
		s.progressWarning(s.nc.Publish(s.ackSubject(reply), []byte("+WPI")))
	}
	s.reportExpired(len(progress.expired))
	return progress.oldest
}

func (s *pullSubscriber) reportExpired(count int) {
	if count == 0 {
		return
	}
	s.expiredUntil.Store(time.Now().Add(laneUnhealthyAfter).UnixNano())
	consumeTelemetry.register(nil, s.subject, 1).expired.Add(uint64(count))
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if time.Since(s.lastExpiredLog) < fetchErrorLogEvery {
		return
	}
	s.lastExpiredLog = time.Now()
	s.log.Warn("pull delivery progress expired; releasing for redelivery",
		zap.String("stream", s.stream), zap.Int("deliveries", count), zap.Uint64("expired", s.expired.Load()))
}

type pullProgress struct {
	replies []string
	expired []string
	oldest  time.Time
	retired []*DeliveryTicket
}

func (s *pullSubscriber) progressReplies(shard *pullPendingShard, now time.Time) pullProgress {
	shard.Lock()
	defer shard.Unlock()
	progress := pullProgress{oldest: now}
	for reply, delivery := range shard.deliveries {
		if delivery.locallyHeld() && s.sourceExpired(delivery, now) {
			delete(shard.deliveries, reply)
			progress.retired = append(progress.retired, delivery)
			continue
		}
		if s.progressExpired(delivery, now) {
			delete(shard.deliveries, reply)
			s.expired.Add(1)
			progress.expired = append(progress.expired, reply)
			continue
		}
		if !delivery.locallyHeld() {
			progress.oldest = minTime(progress.oldest, time.Unix(0, delivery.stored))
		}
		if s.renewProgress(delivery, now) {
			progress.replies = append(progress.replies, reply)
		}
	}
	if len(shard.deliveries) == 0 {
		shard.deliveries = nil
	}
	return progress
}

func (s *pullSubscriber) renewProgress(delivery *DeliveryTicket, now time.Time) bool {
	if s.push != nil && !delivery.handed {
		return false
	}
	age := now.Sub(time.Unix(0, delivery.stored))
	if age-time.Duration(delivery.touched)*time.Millisecond < s.progressInterval(delivery) {
		return false
	}
	delivery.touched = int32(age.Milliseconds())
	return true
}

func (s *pullSubscriber) progressInterval(delivery *DeliveryTicket) time.Duration {
	if delivery.reporterOnly() {
		return 2 * s.desired.AckWait / 3
	}
	return s.desired.AckWait / 3
}

func (s *pullSubscriber) sourceExpired(delivery *DeliveryTicket, now time.Time) bool {
	horizon := time.Duration(s.horizon.Load())
	return horizon > 0 && now.Sub(time.Unix(0, delivery.stored)) >= horizon
}

func (s *pullSubscriber) progressExpired(delivery *DeliveryTicket, now time.Time) bool {
	if delivery.locallyHeld() {
		return false
	}
	age := now.Sub(time.Unix(0, delivery.stored))
	if age-time.Duration(delivery.received)*time.Millisecond >= 2*s.desired.AckWait {
		return true
	}
	horizon := time.Duration(s.horizon.Load())
	if horizon <= 0 {
		return false
	}
	margin := min(4*s.desired.AckWait/3, horizon*3/4)
	cutoff := horizon - margin
	if time.Duration(delivery.received)*time.Millisecond >= cutoff {
		cutoff = horizon
	}
	return age >= cutoff
}

func (delivery *DeliveryTicket) locallyHeld() bool {
	return delivery.held && delivery.executed
}

func (delivery *DeliveryTicket) reporterOnly() bool {
	return delivery.locallyHeld() && delivery.async == 0
}

func (s *pullSubscriber) progressWarning(err error) {
	if err == nil {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if time.Since(s.lastProgressLog) < fetchErrorLogEvery {
		return
	}
	s.lastProgressLog = time.Now()
	s.log.Warn("lane operation failed", zap.String("consumer", s.name), zap.Error(err))
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *pullSubscriber) reportHorizon(oldest time.Time) {
	horizon := time.Duration(s.horizon.Load())
	lag := time.Since(oldest)
	near := horizon > 0 && lag >= horizon*3/4
	previous := s.lagged.Swap(near)
	if near && !previous {
		s.log.Error("consumer lag approaching retention horizon", zap.String("stream", s.stream),
			zap.Duration("lag", lag), zap.Duration("horizon", horizon))
	}
}
