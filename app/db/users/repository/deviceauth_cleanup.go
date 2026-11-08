// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent/authorizationcode"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/pkg/db"
)

// CleanupDeviceAuth prunes expired single-use codes and long-settled device
// grants, mirroring the service's other periodic-sweep cleanup pattern.
func (r *Users) CleanupDeviceAuth(ctx context.Context, now time.Time) (int, error) {
	codes, err := r.deleteExpiredAuthorizationCodes(ctx, now)
	if err != nil {
		return codes, err
	}
	grants, err := r.deleteStaleDeviceGrants(ctx, now)
	if err != nil {
		return codes + grants, err
	}
	proofs, err := r.deleteExpiredDeviceProofs(ctx, now)
	return codes + grants + proofs, err
}

func (r *Users) deleteExpiredAuthorizationCodes(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-deviceCleanupCodeGrace)
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.AuthorizationCode.Delete().Where(authorizationcode.ExpiresAtLT(cutoff)).Exec(ctx)
	})
}

func (r *Users) deleteStaleDeviceGrants(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-deviceCleanupGrantRetain)
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.DeviceGrant.Delete().Where(devicegrant.Or(
			devicegrant.RevokedAtLT(cutoff),
			devicegrant.RefreshAbsoluteExpiresAtLT(cutoff),
		)).Exec(ctx)
	})
}
