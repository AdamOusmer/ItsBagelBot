// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type hotLaneState struct {
	alignment laneAlignment
	paused    atomic.Bool
	ahead     lanePosition
	idle      int
	idleSince time.Time
	otherSeen laneProgress
	queueErr  error
}

type lanePosition struct {
	created   time.Time
	delivered uint64
	sequence  uint64
}

type laneProgress struct {
	created   time.Time
	delivered uint64
	acked     uint64
	pending   int
}

func (s *pullSubscriber) heartbeatInterval() time.Duration {
	return max(s.desired.AckWait/6, time.Millisecond)
}

func (s *pullSubscriber) markLaneError() {
	s.errSince.CompareAndSwap(0, time.Now().UnixNano())
}

func (h *hotLaneBinding) tick() {
	if h.closed.Load() {
		return
	}
	h.inspectQueue()
	ctx, cancel := context.WithTimeout(h.ctx, min(h.heartbeatInterval(), time.Second))
	defer cancel()
	info, err := h.info(ctx, hotDurable(h.name))
	if err != nil {
		h.ahead = lanePosition{}
		h.markLaneError()
		h.progressWarning(err)
		return
	}
	if info == nil {
		h.ahead = lanePosition{}
		h.markLaneError()
		h.rebuildConsumer()
		return
	}
	h.paused.Store(info.Paused)
	stream := h.refreshStream()
	if stream == nil {
		h.ahead = lanePosition{}
		return
	}
	h.reconcileState(stream, info)
}

func (h *hotLaneBinding) reconcileState(stream *nats.StreamInfo, info *jsapi.ConsumerInfo) {
	if info.Paused {
		h.alignment.watch = leaderWatch{}
		h.ahead = lanePosition{}
		h.tryTakeover()
		return
	}
	h.idle = 0
	h.idleSince = time.Time{}
	if h.confirmAhead(info, stream.State.LastSeq) {
		h.changeLane(h.repairAhead)
		h.ahead = lanePosition{}
		return
	}
	if stream.Cluster != nil {
		h.alignment.reconcile(h, stream.Cluster.Leader, info)
	}
	if h.push != nil {
		h.inspectPush(info)
	}
}

func (h *hotLaneBinding) inspectQueue() {
	h.queueErr = nil
	if h.push != nil {
		h.queueErr = h.push.inspectQueue()
	}
}

func (h *hotLaneBinding) inspectPush(info *jsapi.ConsumerInfo) {
	if h.queueErr != nil {
		h.markLaneError()
		return
	}
	if err := h.push.inspect(info); err != nil {
		h.markLaneError()
		return
	}
	h.noteFetchProgress()
}

func (h *hotLaneBinding) tryTakeover() {
	ctx, cancel := context.WithTimeout(h.ctx, min(h.heartbeatInterval(), time.Second))
	defer cancel()
	other, err := h.info(ctx, h.other)
	if err != nil {
		h.idle = 0
		h.idleSince = time.Time{}
		h.markLaneError()
		h.progressWarning(err)
		return
	}
	h.noteFetchProgress()
	h.idle = nextStreak(h.idle, h.peerIdle(other))
	if h.idle >= 2 {
		h.changeLane(h.takeover)
	}
}

func (h *hotLaneBinding) peerIdle(other *jsapi.ConsumerInfo) bool {
	if other == nil {
		return true
	}
	if other.Config.DeliverSubject != "" {
		return !other.PushBound
	}
	return h.pullPeerIdle(other)
}

func (h *hotLaneBinding) pullPeerIdle(other *jsapi.ConsumerInfo) bool {
	progress := laneProgress{other.Created, other.Delivered.Consumer, other.AckFloor.Consumer, other.NumAckPending}
	unchanged := progress == h.otherSeen
	h.otherSeen = progress
	if !unchanged || other.NumWaiting != 0 {
		h.idleSince = time.Time{}
		return false
	}
	if h.idleSince.IsZero() {
		h.idleSince = time.Now()
	}
	return time.Since(h.idleSince) > other.Config.AckWait
}

func nextStreak(streak int, match bool) int {
	if !match {
		return 0
	}
	return min(streak+1, 2)
}

func (h *hotLaneBinding) takeover(ctx context.Context) error {
	info, err := h.info(ctx, hotDurable(h.name))
	if err != nil {
		return err
	}
	if info == nil || !info.Paused {
		return nil
	}
	other, err := h.info(ctx, h.other)
	if err != nil || !h.peerIdle(other) {
		return err
	}
	if err := validateLaneShape(info.Config, h.desired); err != nil {
		return err
	}
	return h.replaceDurable(ctx, h.createFromPeer)
}

func laneAhead(info *jsapi.ConsumerInfo, last uint64) bool {
	return info.Delivered.Stream > last || info.Config.OptStartSeq > last+1
}

func positionOfLane(info *jsapi.ConsumerInfo) lanePosition {
	return lanePosition{info.Created, info.Delivered.Consumer, info.Delivered.Stream}
}

func (h *hotLaneBinding) confirmAhead(info *jsapi.ConsumerInfo, last uint64) bool {
	if !laneAhead(info, last) {
		h.ahead = lanePosition{}
		return false
	}
	position := positionOfLane(info)
	unchanged := h.ahead == position
	h.ahead = position
	return unchanged
}

func (h *hotLaneBinding) repairAhead(ctx context.Context) error {
	info, err := h.info(ctx, hotDurable(h.name))
	if err != nil || info == nil {
		return err
	}
	if positionOfLane(info) != h.ahead {
		return nil
	}
	stream, err := h.letters.js.StreamInfo(h.stream, nats.Context(ctx))
	if err != nil {
		return err
	}
	if info.Paused || !laneAhead(info, stream.State.LastSeq) {
		return nil
	}
	if err := validateLaneShape(info.Config, h.desired); err != nil {
		return err
	}
	return h.replaceDurable(ctx, func(ctx context.Context) error { return h.activate(ctx, h.desired) })
}

func (h *hotLaneBinding) replaceDurable(ctx context.Context, create func(context.Context) error) error {
	if err := h.holdResetInterest(ctx); err != nil {
		return err
	}
	if err := deletePullConsumer(ctx, h.js, h.stream, h.name); err != nil {
		return err
	}
	return create(ctx)
}

func (h *hotLaneBinding) holdResetInterest(ctx context.Context) error {
	stage := pullConsumerConfig(h.subject, h.name+"_reset_staging")
	_, err := h.js.CreateConsumer(ctx, h.stream, stage)
	return err
}

func (h *hotLaneBinding) changeLane(change func(context.Context) error) {
	ctx, cancel := context.WithTimeout(h.ctx, pullProvisionTimeout)
	defer cancel()
	err := h.withClaim(ctx, change)
	if err == nil {
		return
	}
	h.markLaneError()
	h.progressWarning(err)
}
