// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/outgress"
	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"

	"go.uber.org/zap"
)

type liveRPC struct {
	runtime       channelruntime.Reader
	store         *projection.Store
	pub           bus.Publisher
	systemSubject string
	log           *zap.Logger
}

type LiveDependencies struct {
	Store   *projection.Store
	Pub     bus.Publisher
	Runtime channelruntime.Reader
}

func SubscribeLive(w bus.RPCWiring, d LiveDependencies, subject, systemSubject string) error {
	l := &liveRPC{store: d.Store, pub: d.Pub, systemSubject: systemSubject, log: w.Log, runtime: d.Runtime}
	return bus.QueueSubscribeJSON[projectorrpc.LiveRequest, projectorrpc.LiveReply](w.NC, subject, w.Queue, 1500*time.Millisecond, w.App, w.Log, l.handleGet)
}

func (l *liveRPC) handleGet(ctx context.Context, req projectorrpc.LiveRequest) projectorrpc.LiveReply {
	log := monitor.TxnLogger(ctx, l.log)
	if req.BroadcasterID == "" {
		return projectorrpc.LiveReply{Error: "bad request"}
	}
	id, err := strconv.ParseUint(req.BroadcasterID, 10, 64)
	if err != nil {
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Error: "bad request"}
	}

	state, err := l.store.GetStreamLive(ctx, id)
	if err != nil {
		log.Warn("live rpc: store read failed", zap.Uint64("broadcaster_id", id), zap.Error(err))
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: false, Known: false}
	}
	if state.Known {
		return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: state.Live, Known: true, Version: state.Version}
	}

	l.escalate(ctx, req.BroadcasterID)
	return projectorrpc.LiveReply{BroadcasterID: req.BroadcasterID, Live: false, Known: false}
}

func (l *liveRPC) escalate(ctx context.Context, broadcasterID string) {
	if l.pub == nil || l.systemSubject == "" {
		return
	}
	admission, allowed := l.claimEscalation(ctx, broadcasterID)
	if !allowed {
		return
	}
	body, err := codec.Marshal(outgress.StreamStatusJob{BroadcasterID: broadcasterID})
	if err != nil {
		return
	}
	if err := bus.PublishJSON(ctx, l.pub, l.systemSubject, outgress.Message{
		AccountCreatedAt:    admission.AccountCreatedAt,
		AdmissionGeneration: admission.State.AdmissionGeneration,
		Type:                outgress.TypeStreamStatus,
		BroadcasterID:       broadcasterID,
		Payload:             body,
	}); err != nil {
		l.log.Warn("live rpc: failed to escalate to outgress", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

func (l *liveRPC) claimEscalation(ctx context.Context, broadcasterID string) (channelruntime.Snapshot, bool) {
	if l.runtime == nil {
		return channelruntime.Snapshot{}, true
	}
	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		return channelruntime.Snapshot{}, false
	}
	admission, err := l.runtime.Get(ctx, id)
	if err != nil || !admission.AllowsRuntime() {
		return admission, false
	}
	won, err := l.store.ClaimUnknownLive(ctx, id)
	return admission, err == nil && won
}
