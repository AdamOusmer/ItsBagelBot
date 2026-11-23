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

const snapshotEvents = 100

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

// Snapshot is the channel's folded state and the OVERLAY_FEED sequence it includes.
func (s *Service) Snapshot(ctx context.Context, userID string) (overlaysrpc.FeedSnapshotReply, error) {
	c, st := channel(userID), s.store()
	// The sequence is read before the state: a frame published in between is then delivered twice, never missed.
	seq, err := st.seq(ctx, c)
	if err != nil {
		return overlaysrpc.FeedSnapshotReply{}, err
	}
	session, err := st.session(ctx, c)
	if err != nil {
		return overlaysrpc.FeedSnapshotReply{}, err
	}
	snapshot, err := s.snapshotOf(ctx, c, session)
	return overlaysrpc.FeedSnapshotReply{Snapshot: snapshot, StreamSeq: seq}, err
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
	recent, err := s.store().recent(ctx, c, session, snapshotEvents)
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
