// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// streamInfoRPC answers the Overview dashboard's stream-info query. Unlike
// liveRPC it never escalates to outgress on a cold read: a cold stream-info
// row just means lane B (the projector's stream.online/offline handler) has
// not written one yet, and there is no cheap single Twitch call that
// backfills title/game/viewer counts the way stream_status backfills live.
// It answers whatever the store has, not-known included.
type streamInfoRPC struct {
	store *projection.Store
	log   *zap.Logger
}

// SubscribeStreamInfo registers the projector stream-info verb on subject.
func SubscribeStreamInfo(nc *nats.Conn, store *projection.Store, subject, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	s := &streamInfoRPC{store: store, log: log}
	return bus.QueueSubscribeJSON[projectorrpc.StreamInfoRequest, projectorrpc.StreamInfoReply](nc, subject, queueGroup, 1500*time.Millisecond, app, log, s.handleGet)
}

func (s *streamInfoRPC) handleGet(ctx context.Context, req projectorrpc.StreamInfoRequest) projectorrpc.StreamInfoReply {
	log := monitor.TxnLogger(ctx, s.log)
	if req.BroadcasterID == "" {
		return projectorrpc.StreamInfoReply{Error: "bad request"}
	}

	info, known, err := s.store.GetStreamInfo(ctx, req.BroadcasterID)
	if err != nil {
		// Valkey error: do not escalate, just answer not-known.
		log.Warn("stream info rpc: store read failed", zap.String("broadcaster_id", req.BroadcasterID), zap.Error(err))
		return projectorrpc.StreamInfoReply{BroadcasterID: req.BroadcasterID, Known: false}
	}

	return projectorrpc.StreamInfoReply{
		BroadcasterID: req.BroadcasterID,
		Title:         info.Title,
		GameName:      info.GameName,
		ViewerCount:   info.ViewerCount,
		PeakViewers:   info.PeakViewers,
		StartedAt:     info.StartedAt,
		EndedAt:       info.EndedAt,
		Known:         known,
	}
}
