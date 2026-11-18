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
	idle      int
	otherSeen laneProgress
}

type laneProgress struct {
	delivered uint64
	acked     uint64
}

func (s *pullSubscriber) heartbeatInterval() time.Duration {
	return max(s.desired.AckWait/6, time.Millisecond)
}

func (s *pullSubscriber) markLaneError() {
	s.errSince.CompareAndSwap(0, time.Now().UnixNano())
}

func (h *hotLaneBinding) tick(stream *nats.StreamInfo) {
	if h.closed.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(h.ctx, min(h.heartbeatInterval(), time.Second))
	defer cancel()
	info, err := h.info(ctx, hotDurable(h.name))
	if err != nil {
		h.markLaneError()
		h.progressWarning(err)
		return
	}
	if info == nil {
		h.markLaneError()
		h.rebuildConsumer()
		return
	}
	h.paused.Store(info.Paused)
	h.reconcileState(ctx, stream, info)
}

func (h *hotLaneBinding) reconcileState(ctx context.Context, stream *nats.StreamInfo, info *jsapi.ConsumerInfo) {
	if info.Paused {
		h.alignment.watch = leaderWatch{}
		h.tryTakeover(ctx)
		return
	}
	h.idle = 0
	if laneAhead(info, stream.State.LastSeq) {
		h.changeLane(h.repairAhead)
		return
	}
	if stream.Cluster != nil {
		h.alignment.reconcile(h, stream.Cluster.Leader, info)
	}
	if h.push != nil {
		h.inspectPush(info)
	}
}

func (h *hotLaneBinding) inspectPush(info *jsapi.ConsumerInfo) {
	if err := h.push.inspect(info); err != nil {
		h.markLaneError()
		return
	}
	h.noteFetchProgress()
}

func (h *hotLaneBinding) tryTakeover(ctx context.Context) {
	other, err := h.info(ctx, h.other)
	if err != nil {
		h.idle = 0
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
	progress := laneProgress{other.Delivered.Stream, other.AckFloor.Stream}
	unchanged := progress == h.otherSeen
	h.otherSeen = progress
	return unchanged && !other.PushBound && other.NumWaiting == 0
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

func (h *hotLaneBinding) repairAhead(ctx context.Context) error {
	info, err := h.info(ctx, hotDurable(h.name))
	if err != nil || info == nil {
		return err
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
