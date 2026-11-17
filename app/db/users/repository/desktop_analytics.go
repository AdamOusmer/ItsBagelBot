// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

func (r *Users) tallyDesktopGrants(ctx context.Context, tally analytics.Tally) (analytics.Tally, error) {
	now := time.Now()
	for _, platform := range catalog.Enum(catalog.KindPlatform) {
		n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
			return r.client.DeviceGrant.Query().Where(
				devicegrant.PlatformEQ(devicegrant.Platform(platform)), devicegrant.RevokedAtIsNil(),
				devicegrant.RefreshSlidingExpiresAtGT(now), devicegrant.RefreshAbsoluteExpiresAtGT(now),
				devicegrant.HasUserWith(user.TestAccount(false), user.Banned(false)),
			).Count(ctx)
		})
		if err != nil {
			return nil, err
		}
		tally.AddN(int64(n), catalog.DesktopGrants, platform)
	}
	return tally, nil
}
