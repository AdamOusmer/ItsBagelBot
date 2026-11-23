// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package livefeed

import (
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

// Snapshot is the channel's folded state and every event logged in its current session.
func (s *Service) Snapshot(ctx context.Context, userID string) (overlaysrpc.FeedSnapshotReply, error) {
	c := channel(userID)
	session, err := s.store().session(ctx, c)
	if err != nil {
		return overlaysrpc.FeedSnapshotReply{}, err
	}
	snapshot, err := s.snapshotOf(ctx, c, session)
	return overlaysrpc.FeedSnapshotReply{Snapshot: snapshot}, err
}

func (s *Service) snapshotOf(ctx context.Context, c channel, session string) (feed.Snapshot, error) {
	totals, err := s.totals(ctx, c)
	if err != nil {
		return feed.Snapshot{}, err
	}
	stats, err := s.store().stats(ctx, c, session, totals)
	if err != nil {
		return feed.Snapshot{}, err
	}
	recent, err := s.store().recent(ctx, c, session)
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
	events := make([]feed.Delivery, len(recent))
	for i, event := range recent {
		events[i] = feed.Delivery{Event: event}
	}
	return feed.Snapshot{Stats: stats, Subathon: clock, Control: control, Events: events}, nil
}
