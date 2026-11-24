// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
	"cmp"
	"context"

	"ItsBagelBot/internal/domain/overlays/feed"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/valkey"
)

func (s *Service) controlDocument(c channel) valkey.CASDocument {
	return valkey.CASDocument{Key: c.control(), TTL: controlTTL}
}

func (s *Service) readControl(ctx context.Context, c channel) (feed.Control, error) {
	raw, err := s.controlDocument(c).Read(ctx, s.Client)
	var control feed.Control
	if err != nil || raw == "" {
		return control, err
	}
	return control, codec.Unmarshal([]byte(raw), &control)
}

// Snapshot is the channel's folded state with its credit lists and the newest events of its current session, events of them (the default when 0).
func (s *Service) Snapshot(ctx context.Context, userID string, events int) (overlaysrpc.FeedSnapshotReply, error) {
	if err := s.requireAlpha(ctx, userID); err != nil {
		return overlaysrpc.FeedSnapshotReply{}, err
	}
	c := channel(userID)
	session, err := s.store().session(ctx, c)
	if err != nil {
		return overlaysrpc.FeedSnapshotReply{}, err
	}
	snapshot, err := s.snapshotOf(ctx, c, session, int64(cmp.Or(events, overlaysrpc.DefaultSnapshotEvents)))
	return overlaysrpc.FeedSnapshotReply{Snapshot: snapshot}, err
}

func (s *Service) snapshotOf(ctx context.Context, c channel, session sessionID, events int64) (feed.Snapshot, error) {
	totals, err := s.totals(ctx, c)
	if err != nil {
		return feed.Snapshot{}, err
	}
	stats, err := s.store().stats(ctx, c, session, totals)
	if err != nil {
		return feed.Snapshot{}, err
	}
	if stats.Credits, err = s.store().credits(ctx, c, session); err != nil {
		return feed.Snapshot{}, err
	}
	recent, err := s.store().recent(ctx, c, session, events)
	if err != nil {
		return feed.Snapshot{}, err
	}
	clock, err := s.readClock(ctx, c)
	if err != nil {
		return feed.Snapshot{}, err
	}
	control, err := s.readControl(ctx, c)
	if err != nil {
		return feed.Snapshot{}, err
	}
	deliveries := make([]feed.Delivery, len(recent))
	for i, event := range recent {
		deliveries[i] = feed.Delivery{Event: event}
	}
	return feed.Snapshot{Stats: stats, Subathon: clock, Control: control, Events: deliveries}, nil
}
