// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"context"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
)

type SnapshotRequest struct {
	BroadcasterID uint64
	Owner         string
	ExpiresAt     time.Time
}

type SnapshotRef = loyaltyrpc.SnapshotRef

func (l *LoyaltyRPC) FreezeGet(ctx context.Context, channel uint64) (loyaltyrpc.FreezeView, error) {
	return requestLoyalty[loyaltyrpc.FreezeView](ctx, l, loyaltyrpc.VerbFreezeGet, loyaltyrpc.FreezeGetRequest{UserID: fmtID(channel)})
}

func (l *LoyaltyRPC) FreezeSet(ctx context.Context, channel uint64, minutes int, actor uint64) (loyaltyrpc.FreezeTimes, error) {
	req := loyaltyrpc.FreezeSetRequest{UserID: fmtID(channel), Minutes: minutes, ActorID: fmtID(actor)}
	return requestLoyalty[loyaltyrpc.FreezeTimes](ctx, l, loyaltyrpc.VerbFreezeSet, req)
}

func (l *LoyaltyRPC) FreezeRelease(ctx context.Context, channel, actor uint64) (bool, error) {
	req := loyaltyrpc.FreezeReleaseRequest{UserID: fmtID(channel), ActorID: fmtID(actor)}
	reply, err := requestLoyalty[loyaltyrpc.FreezeReleased](ctx, l, loyaltyrpc.VerbFreezeRelease, req)
	return reply.Released, err
}

func (l *LoyaltyRPC) SnapshotCreate(ctx context.Context, req SnapshotRequest) (SnapshotRef, error) {
	wire := loyaltyrpc.SnapshotCreateRequest{UserID: fmtID(req.BroadcasterID), Owner: req.Owner, ExpiresAt: req.ExpiresAt.UnixMilli()}
	return requestLoyalty[SnapshotRef](ctx, l, loyaltyrpc.VerbSnapshotCreate, wire)
}

func (l *LoyaltyRPC) SnapshotDrop(ctx context.Context, channel uint64, snapshotID string) error {
	req := loyaltyrpc.SnapshotDropRequest{UserID: fmtID(channel), SnapshotID: snapshotID}
	_, err := requestLoyalty[loyaltyrpc.SnapshotDropped](ctx, l, loyaltyrpc.VerbSnapshotDrop, req)
	return err
}

func (s *ValkeyLoyaltyStore) FreezeSet(ctx context.Context, channel uint64, minutes int, actor uint64) (loyaltyrpc.FreezeTimes, error) {
	view, err := s.rpc.FreezeSet(ctx, channel, minutes, actor)
	if err == nil {
		s.invalidateTop(cache.Event{ID: channel})
	}
	return view, err
}

func (s *ValkeyLoyaltyStore) FreezeGet(ctx context.Context, channel uint64) (loyaltyrpc.FreezeView, error) {
	return s.rpc.FreezeGet(ctx, channel)
}

func (s *ValkeyLoyaltyStore) FreezeRelease(ctx context.Context, channel, actor uint64) (bool, error) {
	released, err := s.rpc.FreezeRelease(ctx, channel, actor)
	if err == nil && released {
		s.invalidateTop(cache.Event{ID: channel})
	}
	return released, err
}

func (s *ValkeyLoyaltyStore) SnapshotCreate(ctx context.Context, req SnapshotRequest) (SnapshotRef, error) {
	return s.rpc.SnapshotCreate(ctx, req)
}

func (s *ValkeyLoyaltyStore) SnapshotDrop(ctx context.Context, channel uint64, snapshotID string) error {
	return s.rpc.SnapshotDrop(ctx, channel, snapshotID)
}

func freezeTTL(stamp loyaltyrpc.FreezeStamp, base time.Duration) time.Duration {
	if stamp.ExpiresAt == 0 {
		return base
	}
	return max(time.Nanosecond, min(base-time.Nanosecond, time.Until(time.UnixMilli(stamp.ExpiresAt))))
}

func (s *ValkeyLoyaltyStore) ExtendSnapshot(ctx context.Context, ext draw.SnapshotExtension) (bool, error) {
	req := loyaltyrpc.SnapshotExtendRequest{UserID: fmtID(uint64(ext.Channel)), SnapshotID: ext.ID, ExpiresAt: ext.ExpiresAt.UnixMilli()}
	reply, err := requestLoyalty[loyaltyrpc.SnapshotExtended](ctx, s.rpc, loyaltyrpc.VerbSnapshotExtend, req)
	return reply.Extended, err
}
