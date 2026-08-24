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

// Push status protocol (nats-server 2.14 consumer.go, nats.go jetstream/push.go).
// A status delivery carries no payload; the fleet answers two of them:
//
//	100 "FlowControl Request"  arrives inline in the delivery stream, reply set
//	                           to the server's current flow-control id.
//	100 "Idle Heartbeat"       carries Nats-Consumer-Stalled only while a
//	                           flow-control response is still outstanding.
//
// Under AckFlowControl the response headers ARE the acknowledgement: the server
// reads Nats-Last-Consumer/Nats-Last-Stream off the response and advances the
// replicated ack floor to that stream sequence in one operation.
const (
	statusHeader          = "Status"
	descriptionHeader     = "Description"
	lastConsumerHeader    = "Nats-Last-Consumer"
	lastStreamHeader      = "Nats-Last-Stream"
	consumerStalledHeader = "Nats-Consumer-Stalled"

	controlStatus = "100"
)

const (
	// The server pins this ack policy's heartbeat to exactly one second, so three
	// missed beats is the earliest safe conclusion that the consumer behind this
	// delivery subject is gone. A push consumer learns about deletion only from
	// heartbeat loss: no 404 and no 409 is ever published to a push delivery
	// subject in 2.14.3.
	flowHeartbeatGrace = 3 * time.Second
	flowWatchdogTick   = time.Second

	// flowWedgeStreak is how many consecutive stalled heartbeats, with no delivery
	// between any two of them, prove the flow-control conversation has gone
	// one-way. The heartbeat watchdog structurally cannot see this failure: the
	// server heartbeats a STALLED consumer once a second indefinitely, so
	// lastControl stays fresh for as long as the lane is frozen. Ten seconds of
	// stall with nothing delivered is past any window the server would reopen on
	// its own.
	flowWedgeStreak = 10

	// flowSessionResetGap is the backwards jump in consumer sequence that can only
	// be a new server-side session. A delete + recreate restarts the consumer
	// sequence at 1 while the stream sequence keeps climbing, so anything further
	// back than a whole ack window is a reset rather than reordering.
	flowSessionResetGap = flowMaxAckPending
)

// flowPosition is one immutable receipt cursor sample.
type flowPosition struct {
	consumer uint64
	stream   uint64
}

// flowCursor tracks the highest delivery this process has actually taken off
// the wire. The flow response never claims more than this, so the replicated
// ack floor can never move past a message the pod did not receive.
//
// The pair is two atomics rather than one word because it is read far more
// often than written: snapshot stays lock-free, loading consumer before
// stream so any mixed pair it catches under-claims an ack floor and never
// over-claims one. Writers take mu: without it, record's CAS-from-observed
// retry could reload a cursor reset() had just zeroed and republish a dead
// session's stream sequence onto the fresh pair — prevStream 0 passes both
// gates for any live-looking sample, and recovery would then start past
// messages no session ever received. reset is rare (heartbeat-loss
// reprovision) and record's critical section is two loads and two stores, so
// the uncontended mutex costs nothing next to the per-delivery network work.
type flowCursor struct {
	mu       sync.Mutex
	consumer atomic.Uint64
	stream   atomic.Uint64
}

// record gates on the STREAM sequence, which is monotonic across a consumer
// delete + recreate. The consumer sequence is not: the server restarts it at 1
// whatever start sequence the recreated consumer opens at, so a cursor gated on
// it rejects every delivery from the new session forever and freezes the ack
// floor at MaxAckPending with no redelivery and no error. It returns false for a
// sample that did not advance the cursor.
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

// isSessionReset recognises the server's own reset signature: a consumer
// sequence further back than a whole ack window cannot be reordering.
func isSessionReset(stored, consumer uint64) bool {
	return stored > consumer &&
		stored-consumer > flowSessionResetGap
}

func (c *flowCursor) snapshot() flowPosition {
	// Consumer-before-stream: worst case this returns last window's consumer
	// beside the fresh stream, which only under-claims an ack floor.
	consumer := c.consumer.Load()
	return flowPosition{consumer: consumer, stream: c.stream.Load()}
}

// reset drops the cursor when this process recreates its own consumer. The new
// consumer's sequences start over, so keeping the old pair would answer flow
// control with an ack floor from a session the server no longer has. mu makes
// the two stores one step against record, so no commit can straddle them and
// leave either word speaking for a session that is gone.
func (c *flowCursor) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumer.Store(0)
	c.stream.Store(0)
}

// flowControlResponse builds the receipt-level acknowledgement for one push
// status message, or nil when the status needs no answer (a heartbeat sent
// while no flow-control window is outstanding).
//
// A response is mandatory even with nothing received: the server reopens the
// delivery window purely on the response arriving, and only reads an ack floor
// out of the headers when they are present.
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

// handleStatus answers control messages and records that the consumer behind
// this delivery subject is alive. Nothing but 100-class control messages is ever
// published to a push delivery subject in 2.14.3 — every 404 and 409 goes to a
// pull request's reply — so a non-100 status is logged rather than treated as a
// deletion signal. Deletion is detected by the heartbeat watchdog instead.
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

// noteStall counts stalled heartbeats that no delivery interrupted. The stalled
// marker says the server is holding the window shut until a flow-control
// response reaches it, so a run of them says this pod's responses are not
// arriving — and the only two causes are an ACL that denies publishing to the
// $JS.FC subject and a response the connection dropped.
//
// It is reported, never repaired. Recreating the consumer cannot grant a
// permission, and a watchdog that recreated on this signal would churn the lane
// once a second for as long as the denial lasted; the ack floor and every
// in-flight delivery would go with it each time. The counter and the log line
// are what an operator needs, and the fix is on the broker's side.
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

// noteDeliveryGap compares what the heartbeat says the server has sent with what
// this process has taken off the wire. A gap wider than the whole ack window
// cannot be traffic in flight: after a leader change the server resumes past its
// last delivered sequence and never redelivers what fell in between, and this ack
// policy has no other redelivery. It is reported rather than repaired —
// $JS.API.CONSUMER.RESET would rewind to the ack floor and replay everything
// already handled since it.
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

// deliveriesWereLost separates a legitimate in-flight window from a real gap.
// The server never has more than MaxAckPending deliveries outstanding, so
// anything beyond that is traffic that will never arrive.
func deliveriesWereLost(sent, received uint64) bool {
	return sent > received+flowMaxAckPending
}

func (s *flowSubscriber) respondFlowControl(control *nats.Msg) {
	response := flowControlResponse(control, s.cursor.snapshot())
	if response == nil {
		return
	}
	if err := s.nc.PublishMsg(response); err != nil {
		// The server reopens the window on its next heartbeat's stalled marker,
		// so a lost response costs latency rather than the binding.
		s.log.Warn("flow-control response failed", zap.String("subject", s.subject), zap.Error(err))
	}
}

// watchHeartbeats is the only way this binding learns its consumer is gone. The
// server pins the heartbeat at one second and publishes no deletion status to a
// push subject, so silence on the delivery subject is the signal: the lane would
// otherwise sit at zero events per second, healthy and Ready, indefinitely.
//
// It detects a DELETED consumer and nothing else. A wedged one heartbeats
// forever, so silence never comes and this watchdog never fires for it; that
// failure belongs to noteStall.
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

// heartbeatLost is the watchdog's decision. The heartbeat is pinned at one
// second for this ack policy, so three missed beats is silence, not jitter.
func heartbeatLost(now time.Time, lastControlUnixNano int64) bool {
	return now.Sub(time.Unix(0, lastControlUnixNano)) > flowHeartbeatGrace
}

// recoverConsumer re-provisions a consumer the watchdog found silent. The
// delivery subject is deterministic, so the existing subscription keeps
// receiving once the consumer is back; single-flighting stops the watchdog from
// racing itself while a slow provision is in progress.
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
	// The recreated consumer restarts its own sequences, so the old pair would
	// answer flow control for a session the server no longer has.
	s.cursor.reset()
	s.lastControl.Store(time.Now().UnixNano())
	s.log.Warn("flow consumer re-provisioned after heartbeat loss",
		zap.String("subject", s.subject), zap.String("consumer", s.consumer))
}
