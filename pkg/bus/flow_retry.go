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
	"github.com/nats-io/nuid"
	"go.uber.org/zap"
)

// RetryCountHeader bounds the flow path's redelivery to a single hop. It is an
// ordinary application header, which is exactly why it can carry the budget: the
// message scheduler strips every Nats-* header from what it emits and carries
// application headers through verbatim, so this count survives the hop while the
// schedule's own control headers do not.
const RetryCountHeader = "Bagelbot-Retry"

const (
	// flowRetryTimeout bounds the request that stages one retry schedule. The
	// retry runs on the handler's own goroutine, so this is the whole cost a
	// failed delivery can impose on the lane.
	flowRetryTimeout = 5 * time.Second

	// retryLanePrefix is the fleet's retry namespace, captured by
	// TWITCH_INGRESS_RETRY. Both the schedule row and the message it emits live
	// under it, because nats-server requires a schedule's Nats-Schedule-Target to
	// collide with a subject of the very stream that stores the schedule
	// (stream.go, JSMessageSchedulesTargetInvalid): a schedule can never deliver
	// into a different stream, so the retry hop cannot land back on the ingress
	// lane itself.
	retryLanePrefix = "twitch.ingress.retry."

	// retryScheduleTTL expires the emitted retry, not the schedule row. A retry
	// nobody consumed within this window is a stale chat event and must not be
	// acted on late; the one-shot row purges itself as it fires.
	//
	// Both this and the delay below are derived from TWITCH_INGRESS.MaxAge, which
	// is 10s and IS the lane's staleness policy — an event the broker would have
	// evicted for being too old to answer does not become answerable by having
	// failed once. The original pair (3s delay, 30s TTL) predates that window and
	// let a retry land ~33s after its failure, on top of the event's own age. Kept
	// well inside it instead: one hop, delivered inside the window, expired at
	// half of it. TWITCH_INGRESS_RETRY.MaxAge is 2m and is unrelated — it bounds
	// the schedule ROW, which has to outlive its own delay to fire at all.
	retryScheduleTTL = "5s"

	defaultFlowRetryDelay = time.Second
)

// RetryLaneSubject is the retry lane a failed hot-lane event is scheduled onto.
// It is the schedule's target subject, and the subject a consumer binds to pick
// the retry back up.
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

// scheduleLaneRetry is the redelivery both receipt-level lane adapters share.
// Neither has per-message pending state to NAK against: the flow lane's server
// does not even subscribe to $JS.ACK for its ack policy, and the pull lane's
// cumulative floor has already moved past the message. Both therefore hand the
// event to the broker's own scheduler instead — a one-shot @at row on the retry
// stream that re-emits the payload onto the retry lane once the delay has
// passed, then purges itself.
//
// It is one function on purpose: the budget check, the schedule shape and the
// PubAck-error reading are the parts that go silently wrong, and a second copy
// of them would be a second place for a retry to be dropped without a trace.
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

// retryScheduleMsg builds the one-shot schedule row. The row's own subject is
// unique per retry because publishing a schedule rolls its subject up: a second
// retry on a shared subject would purge the first one before it ever fired.
//
// No Nats-Schedule-Time-Zone header is set. The server rejects the @at form
// outright when a time zone is present, even "UTC".
func retryScheduleMsg(lane string, wire *nats.Msg, delay time.Duration, now time.Time) *nats.Msg {
	target := RetryLaneSubject(lane)
	schedule := nats.NewMsg(target + "." + nuid.Next())
	schedule.Data = append([]byte(nil), wire.Data...)
	copyApplicationHeaders(schedule.Header, wire.Header)

	// Carry a stable idempotency key across the hop. An ingress-origin event
	// reaches the lane with no Bagelbot-Message-Id, so copyApplicationHeaders had
	// nothing to carry and the re-emitted retry would land under a fresh stream
	// sequence — a different Message.UUID that a consumer's dedup guard could not
	// recognise as the same event. Stamp the delivery's already-resolved identity
	// (the JetStream stream-seq fallback for those events) so Message.UUID stays
	// stable across the retry. A Go-origin header copied above is left untouched.
	if wire.Header.Get(MessageIDHeader) == "" {
		schedule.Header.Set(MessageIDHeader, messageIdentity(wire))
	}

	schedule.Header.Set(jsapi.ScheduleHeader, "@at "+now.Add(delay).UTC().Format(time.RFC3339))
	schedule.Header.Set(jsapi.ScheduleTargetHeader, target)
	schedule.Header.Set(jsapi.ScheduleTTLHeader, retryScheduleTTL)
	schedule.Header.Set(RetryCountHeader, "1")
	return schedule
}

// copyApplicationHeaders carries the fleet's own headers across the hop and
// leaves every Nats-* header behind. The scheduler strips those from what it
// emits anyway, and the expectation headers among them would be re-evaluated
// against the retry stream and reject the publish.
func copyApplicationHeaders(into, from nats.Header) {
	for key, values := range from {
		if len(values) == 0 || strings.HasPrefix(key, "Nats-") {
			continue
		}
		into.Set(key, values[0])
	}
}

// flowRetryDelay is how long a failed event waits before the broker re-emits it.
// It exists to let a transient downstream failure clear, so it is tunable
// without a rebuild.
func flowRetryDelay() time.Duration {
	delay := env.GetDuration("NATS_FLOW_RETRY_DELAY", defaultFlowRetryDelay)
	if delay <= 0 {
		return defaultFlowRetryDelay
	}
	return delay
}

// pubAckError reports a JetStream publish rejection. A schedule the broker
// refuses (schedules disabled, an invalid target, a denied subject) answers with
// a normal PubAck carrying an error, which is silent unless it is read.
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
