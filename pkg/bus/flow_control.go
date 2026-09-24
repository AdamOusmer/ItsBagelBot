// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	statusHeader          = "Status"
	descriptionHeader     = "Description"
	lastConsumerHeader    = "Nats-Last-Consumer"
	lastStreamHeader      = "Nats-Last-Stream"
	consumerStalledHeader = "Nats-Consumer-Stalled"

	controlStatus = "100"
)

const (
	flowHeartbeatGrace = 3 * time.Second
	flowWatchdogTick   = time.Second

	flowWedgeStreak = 10

	flowSessionResetGap = flowMaxAckPending
)

type flowPosition struct {
	consumer uint64
	stream   uint64
}

type flowCursor struct {
	mu       sync.Mutex
	consumer atomic.Uint64
	stream   atomic.Uint64
}

// Gate on the stream sequence: the consumer sequence restarts at 1 and would freeze the ack floor.
func (c *flowCursor) record(consumer, stream uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	prevStream := c.stream.Load()
	if !isSessionReset(c.consumer.Load(), consumer) && stream <= prevStream {
		return false
	}
	c.consumer.Store(consumer)
	c.stream.Store(stream)
	return true
}

func isSessionReset(stored, consumer uint64) bool {
	return stored > consumer &&
		stored-consumer > flowSessionResetGap
}

func (c *flowCursor) snapshot() flowPosition {
	// Load consumer before stream so a torn pair only under-claims the ack floor.
	consumer := c.consumer.Load()
	return flowPosition{consumer: consumer, stream: c.stream.Load()}
}

func (c *flowCursor) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumer.Store(0)
	c.stream.Store(0)
}

func flowControlResponse(control *nats.Msg, position flowPosition) *nats.Msg {
	reply := control.Reply
	if stalled := control.Header.Get(consumerStalledHeader); stalled != "" {
		reply = stalled
	}
	if reply == "" {
		return nil
	}
	response := nats.NewMsg(reply)
	if position.stream == 0 {
		return response
	}
	response.Header.Set(lastConsumerHeader, strconv.FormatUint(position.consumer, 10))
	response.Header.Set(lastStreamHeader, strconv.FormatUint(position.stream, 10))
	return response
}

func (s *flowSubscriber) handleStatus(control *nats.Msg, status string) {
	if status == controlStatus {
		s.lastControl.Store(time.Now().UnixNano())
		s.noteStall(control)
		s.noteDeliveryGap(control)
		s.respondFlowControl(control)
		return
	}
	s.log.Warn("unexpected flow consumer status",
		zap.String("subject", s.subject),
		zap.String("status", status),
		zap.String("description", control.Header.Get(descriptionHeader)))
}

func (s *flowSubscriber) noteStall(control *nats.Msg) {
	fcid := control.Header.Get(consumerStalledHeader)
	if fcid == "" {
		return
	}
	if streak := s.stallStreak.Add(1); streak%flowWedgeStreak == 0 {
		s.log.Error("flow consumer is wedged: stalled heartbeats are going unanswered",
			zap.String("subject", s.subject),
			zap.String("consumer", s.consumer),
			zap.String("flow_control_id", fcid),
			zap.Int64("stalled_heartbeats", streak),
			zap.Int64("wedged_total", s.wedged.Add(1)))
	}
}

func (s *flowSubscriber) noteDeliveryGap(control *nats.Msg) {
	sent, err := strconv.ParseUint(control.Header.Get(lastConsumerHeader), 10, 64)
	if err != nil {
		return
	}
	received := s.cursor.snapshot().consumer
	if !deliveriesWereLost(sent, received) {
		return
	}
	s.log.Warn("flow consumer deliveries were never received",
		zap.String("subject", s.subject),
		zap.Uint64("server_last_consumer", sent),
		zap.Uint64("received_consumer", received))
}

func deliveriesWereLost(sent, received uint64) bool {
	return sent > received+flowMaxAckPending
}

func (s *flowSubscriber) respondFlowControl(control *nats.Msg) {
	response := flowControlResponse(control, s.cursor.snapshot())
	if response == nil {
		return
	}
	if err := s.nc.PublishMsg(response); err != nil {
		s.log.Warn("flow-control response failed", zap.String("subject", s.subject), zap.Error(err))
	}
}

func (s *flowSubscriber) watchHeartbeats() {
	defer s.workers.Done()
	ticker := time.NewTicker(flowWatchdogTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return
		case now := <-ticker.C:
			if heartbeatLost(now, s.lastControl.Load()) {
				s.recoverConsumer()
			}
		}
	}
}

func heartbeatLost(now time.Time, lastControlUnixNano int64) bool {
	return now.Sub(time.Unix(0, lastControlUnixNano)) > flowHeartbeatGrace
}

func (s *flowSubscriber) recoverConsumer() {
	if !s.recovering.CompareAndSwap(false, true) {
		return
	}
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()
		defer s.recovering.Store(false)
		s.reprovision()
	}()
}

func (s *flowSubscriber) reprovision() {
	desired := recoveryFlowConsumerConfig(s.binding(), s.cursor.snapshot())
	if _, err := ensureFlowConsumer(s.nc, s.binding(), desired); err != nil {
		s.log.Error("flow consumer recovery failed", zap.String("subject", s.subject), zap.Error(err))
		return
	}
	s.cursor.reset()
	s.lastControl.Store(time.Now().UnixNano())
	s.log.Warn("flow consumer re-provisioned after heartbeat loss",
		zap.String("subject", s.subject), zap.String("consumer", s.consumer))
}
