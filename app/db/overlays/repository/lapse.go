// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"slices"
	"time"

	entoverlay "ItsBagelBot/app/db/overlays/ent/overlay"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/db"
)

const lapseRetention = overlays.LapseRetentionDays * 24 * time.Hour

func (r *Overlays) CustomOwners(ctx context.Context) ([]uint64, error) {
	ids, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.KindEQ(entoverlay.KindCustom)).Select(entoverlay.FieldUserID).Ints)
	owners := make([]uint64, len(ids))
	for i, id := range ids {
		owners[i] = uint64(id)
	}
	slices.Sort(owners)
	return slices.Compact(owners), err
}

// MarkLapsed keeps the first lapse time, so repeated sweeps and replicas agree on one start.
func (r *Overlays) MarkLapsed(ctx context.Context, userID uint64, now time.Time) error {
	update := r.client.Overlay.Update().
		Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom), entoverlay.LapsedAtIsNil()).
		SetLapsedAt(now.UTC().Truncate(time.Microsecond))
	_, err := db.WithQuery(ctx, update.Save)
	return err
}

func (r *Overlays) ClearLapsed(ctx context.Context, userID uint64) error {
	update := r.client.Overlay.Update().
		Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom), entoverlay.LapsedAtNotNil()).
		ClearLapsedAt()
	_, err := db.WithQuery(ctx, update.Save)
	return err
}

// PurgeLapsed deletes custom overlays lapsed for the retention period, then the account's assets once no custom overlay still references them.
func (r *Overlays) PurgeLapsed(ctx context.Context, userID uint64, now time.Time) error {
	expired := entoverlay.LapsedAtLTE(now.Add(-lapseRetention).UTC().Truncate(time.Microsecond))
	rows, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom), expired).All)
	if err != nil || len(rows) == 0 {
		return err
	}
	for _, row := range rows {
		if err := r.removeOverlay(ctx, cloudOverlayOf(row)); err != nil && !errors.Is(err, ErrRevisionConflict) {
			return err
		}
	}
	left, err := db.WithQuery(ctx, r.client.Overlay.Query().Where(entoverlay.UserID(userID), entoverlay.KindEQ(entoverlay.KindCustom)).Count)
	if err != nil || left > 0 {
		return err
	}
	return r.deleteAssets(ctx, userID, now)
}
