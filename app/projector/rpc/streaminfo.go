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

	// Live is a SEPARATE field of the same settings hash, written by the stream
	// event path, so it is read separately here. Reporting the metadata without
	// it would leave the panel permanently showing an offline channel: the
	// metadata alone cannot say whether the stream it describes is the one
	// running now or the last one that ended.
	//
	// The id is re-parsed because the two accessors take different types — the
	// stream-info pair keys by the raw string (parsing internally), GetStreamLive
	// by the parsed uint64. Both land on the same cache.UserKey, so the hash is
	// shared; only the call signatures differ.
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
