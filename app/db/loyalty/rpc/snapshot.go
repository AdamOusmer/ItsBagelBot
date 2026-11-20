// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"time"

	"ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
)

func (l *loyaltyRPC) subscribeSnapshots(w bus.RPCWiring, prefix string) error {
	for verb, register := range map[string]func(string) error{
		loyaltyrpc.VerbSnapshotCreate: func(subject string) error { return bus.Serve(w, subject, l.handleSnapshotCreate) },
		loyaltyrpc.VerbSnapshotDrop:   func(subject string) error { return bus.Serve(w, subject, l.handleSnapshotDrop) },
	} {
		if err := register(prefix + "." + verb); err != nil {
			return err
		}
	}
	return nil
}

func (l *loyaltyRPC) handleSnapshotCreate(ctx context.Context, req loyaltyrpc.SnapshotCreateRequest) loyaltyrpc.SnapshotRef {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.SnapshotRef{Refusal: invalidStandings(err)}
	}
	state, err := l.repo.CreateSnapshot(ctx, repository.SnapshotRequest{
		UserID: userID, Owner: repository.SnapshotOwner(req.Owner), ExpiresAt: time.UnixMilli(req.ExpiresAt),
	})
	if err != nil {
		return loyaltyrpc.SnapshotRef{Refusal: l.fail("loyalty snapshot.create", err).Refusal}
	}
	return loyaltyrpc.SnapshotRef{SnapshotID: string(state.SnapshotID), AsOf: state.AsOf.UnixMilli()}
}

func (l *loyaltyRPC) handleSnapshotDrop(ctx context.Context, req loyaltyrpc.SnapshotDropRequest) loyaltyrpc.SnapshotDropped {
	userID, err := bus.NonZeroUserID(req.UserID)
	if err != nil {
		return loyaltyrpc.SnapshotDropped{Refusal: invalidStandings(err)}
	}
	dropped, err := l.repo.DropSnapshot(ctx, userID, repository.SnapshotID(req.SnapshotID))
	if err != nil {
		return loyaltyrpc.SnapshotDropped{Refusal: l.fail("loyalty snapshot.drop", err).Refusal}
	}
	return loyaltyrpc.SnapshotDropped{Dropped: dropped}
}
