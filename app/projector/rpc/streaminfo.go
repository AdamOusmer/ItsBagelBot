// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"strconv"
	"time"

	projectorrpc "ItsBagelBot/internal/domain/rpc/projector"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/monitor"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type streamInfoRPC struct {
	store *projection.Store
	log   *zap.Logger
}

type StreamInfoDeps struct {
	NC         *nats.Conn
	Store      *projection.Store
	Subject    string
	QueueGroup string
	App        *newrelic.Application
	Log        *zap.Logger
}

func SubscribeStreamInfo(d StreamInfoDeps) error {
	s := &streamInfoRPC{store: d.Store, log: d.Log}
	return bus.QueueSubscribeJSON[projectorrpc.StreamInfoRequest, projectorrpc.StreamInfoReply](
		d.NC, d.Subject, d.QueueGroup, 1500*time.Millisecond, d.App, d.Log, s.handleGet)
}

func (s *streamInfoRPC) handleGet(ctx context.Context, req projectorrpc.StreamInfoRequest) projectorrpc.StreamInfoReply {
	log := monitor.TxnLogger(ctx, s.log)
	if req.BroadcasterID == "" {
		return projectorrpc.StreamInfoReply{Error: "bad request"}
	}

	info, known, err := s.store.GetStreamInfo(ctx, req.BroadcasterID)
	if err != nil {
		log.Warn("stream info rpc: store read failed", zap.String("broadcaster_id", req.BroadcasterID), zap.Error(err))
		return projectorrpc.StreamInfoReply{BroadcasterID: req.BroadcasterID, Known: false}
	}

	live := false
	if id, perr := strconv.ParseUint(req.BroadcasterID, 10, 64); perr == nil {
		got, _, liveErr := s.store.GetStreamLive(ctx, id)
		if liveErr != nil {
			log.Warn("stream info rpc: live read failed", zap.String("broadcaster_id", req.BroadcasterID), zap.Error(liveErr))
		}
		live = got
	}

	return projectorrpc.StreamInfoReply{
		BroadcasterID: req.BroadcasterID,
		Title:         info.Title,
		GameName:      info.GameName,
		ViewerCount:   info.ViewerCount,
		PeakViewers:   info.PeakViewers,
		StartedAt:     info.StartedAt,
		EndedAt:       info.EndedAt,
		Live:          live,
		Known:         known,
	}
}
