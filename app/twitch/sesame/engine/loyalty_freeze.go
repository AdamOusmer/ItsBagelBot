// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
)

type FreezeMinutes int

type SnapshotRequest struct {
	BroadcasterID uint64
	Owner         string
	ExpiresAt     time.Time
}

type SnapshotDropRequest struct {
	Channel    draw.ChannelID
	SnapshotID string
}

type SnapshotRef = loyaltyrpc.SnapshotRef

func (l *LoyaltyRPC) FreezeGet(ctx context.Context, channel draw.ChannelID) (loyaltyrpc.FreezeView, error) {
	return requestLoyalty[loyaltyrpc.FreezeView](ctx, l, loyaltyrpc.VerbFreezeGet, loyaltyrpc.FreezeGetRequest{UserID: fmtID(uint64(channel))})
}

func (l *LoyaltyRPC) FreezeSet(ctx context.Context, channel draw.ChannelID, minutes FreezeMinutes) (loyaltyrpc.FreezeTimes, error) {
	req := loyaltyrpc.FreezeSetRequest{UserID: fmtID(uint64(channel)), Minutes: int(minutes)}
	return requestLoyalty[loyaltyrpc.FreezeTimes](ctx, l, loyaltyrpc.VerbFreezeSet, req)
}

func (l *LoyaltyRPC) FreezeRelease(ctx context.Context, channel draw.ChannelID) (bool, error) {
	req := loyaltyrpc.FreezeReleaseRequest{UserID: fmtID(uint64(channel))}
	reply, err := requestLoyalty[loyaltyrpc.FreezeReleased](ctx, l, loyaltyrpc.VerbFreezeRelease, req)
	return reply.Released, err
}

func (l *LoyaltyRPC) SnapshotCreate(ctx context.Context, req SnapshotRequest) (SnapshotRef, error) {
	wire := loyaltyrpc.SnapshotCreateRequest{UserID: fmtID(req.BroadcasterID), Owner: req.Owner, ExpiresAt: req.ExpiresAt.UnixMilli()}
	return requestLoyalty[SnapshotRef](ctx, l, loyaltyrpc.VerbSnapshotCreate, wire)
}

func (l *LoyaltyRPC) SnapshotDrop(ctx context.Context, req SnapshotDropRequest) error {
	wire := loyaltyrpc.SnapshotDropRequest{UserID: fmtID(uint64(req.Channel)), SnapshotID: req.SnapshotID}
	_, err := requestLoyalty[loyaltyrpc.SnapshotDropped](ctx, l, loyaltyrpc.VerbSnapshotDrop, wire)
	return err
}

func (s *ValkeyLoyaltyStore) FreezeSet(ctx context.Context, channel draw.ChannelID, minutes FreezeMinutes) (loyaltyrpc.FreezeTimes, error) {
	view, err := s.rpc.FreezeSet(ctx, channel, minutes)
	if err == nil {
		s.invalidateTop(cache.Event{ID: uint64(channel)})
	}
	return view, err
}

func (s *ValkeyLoyaltyStore) FreezeGet(ctx context.Context, channel draw.ChannelID) (loyaltyrpc.FreezeView, error) {
	return s.rpc.FreezeGet(ctx, channel)
}

func (s *ValkeyLoyaltyStore) FreezeRelease(ctx context.Context, channel draw.ChannelID) (bool, error) {
	released, err := s.rpc.FreezeRelease(ctx, channel)
	if err == nil && released {
		s.invalidateTop(cache.Event{ID: uint64(channel)})
	}
	return released, err
}

func (s *ValkeyLoyaltyStore) SnapshotCreate(ctx context.Context, req SnapshotRequest) (SnapshotRef, error) {
	return s.rpc.SnapshotCreate(ctx, req)
}

func (s *ValkeyLoyaltyStore) SnapshotDrop(ctx context.Context, req SnapshotDropRequest) error {
	return s.rpc.SnapshotDrop(ctx, req)
}

func freezeTTL(stamp loyaltyrpc.FreezeStamp, base time.Duration) time.Duration {
	if stamp.ExpiresAt == 0 {
		return base
	}
	return max(time.Nanosecond, min(base-time.Nanosecond, time.Until(time.UnixMilli(stamp.ExpiresAt))))
}
