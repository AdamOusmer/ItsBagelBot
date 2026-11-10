// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/authorizationcode"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/app/db/users/ent/predicate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

const controlsRevokedSubjectPrefix = "controls.revoked."

func (r *Users) ListDeviceGrants(ctx context.Context, userID uint64) ([]usersrpc.DeviceGrantView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.DeviceGrant, error) {
		return r.client.DeviceGrant.Query().
			Where(devicegrant.UserIDEQ(userID), devicegrant.RevokedAtIsNil()).
			Order(ent.Desc(devicegrant.FieldCreatedAt)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]usersrpc.DeviceGrantView, 0, len(rows))
	for _, row := range rows {
		out = append(out, usersrpc.DeviceGrantView{
			GrantID: row.GrantID, DeviceID: row.DeviceID, DeviceName: row.DeviceName,
			Platform: string(row.Platform), CreatedAt: row.CreatedAt,
			LastUsedAt: row.LastUsedAt, ExpiresAt: row.RefreshAbsoluteExpiresAt,
		})
	}
	return out, nil
}

// RevokeDeviceGrant revokes exactly one grant owned by userID; a grant_id
// that belongs to someone else, or does not exist, is refused the same way.
func (r *Users) RevokeDeviceGrant(ctx context.Context, userID uint64, grantID string) (string, error) {
	own := predicate.DeviceGrant(devicegrant.And(devicegrant.GrantIDEQ(grantID), devicegrant.UserIDEQ(userID)))
	deviceID, changed, err := r.revokeGrantRow(ctx, own, "user", time.Now().UTC())
	if err != nil {
		return "", err
	}
	if changed {
		r.publishDeviceRevoked(deviceID)
	}
	return deviceID, nil
}

// RevokeAllDeviceGrants wires sign-out-everywhere, ban and erasure to the
// same bulk revoke; it returns every device_id that was newly revoked. Unused
// authorization codes die with the grants, so none can mint a grant afterwards.
func (r *Users) RevokeAllDeviceGrants(ctx context.Context, userID uint64, reason string) ([]string, error) {
	var deviceIDs []string
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
			var err error
			deviceIDs, err = revokeAllInTx(ctx, tx, userID, reason)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	for _, deviceID := range deviceIDs {
		r.publishDeviceRevoked(deviceID)
	}
	return deviceIDs, nil
}

func revokeAllInTx(ctx context.Context, tx *ent.Tx, userID uint64, reason string) ([]string, error) {
	if _, err := lockDeviceOwner(ctx, tx, userID); err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	_, err := tx.AuthorizationCode.Delete().
		Where(authorizationcode.UserIDEQ(userID), authorizationcode.ConsumedAtIsNil()).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.DeviceGrant.Query().
		Where(devicegrant.UserIDEQ(userID), devicegrant.RevokedAtIsNil()).
		All(ctx)
	if err != nil || len(rows) == 0 {
		return nil, err
	}

	ids := make([]int, 0, len(rows))
	deviceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		deviceIDs = append(deviceIDs, row.DeviceID)
	}
	_, err = tx.DeviceGrant.Update().
		Where(devicegrant.IDIn(ids...), devicegrant.RevokedAtIsNil()).
		SetRevokedAt(time.Now().UTC()).SetRevokedReason(reason).Save(ctx)
	if err != nil {
		return nil, err
	}
	return deviceIDs, nil
}

// revokeGrantByGrantID is the internal replay-detection path: it has no
// owner check because it fires from code/refresh reuse, not a user action.
func (r *Users) revokeGrantByGrantID(ctx context.Context, grantID, reason string) (string, error) {
	deviceID, changed, err := r.revokeGrantRow(ctx, devicegrant.GrantIDEQ(grantID), reason, time.Now().UTC())
	if err != nil {
		return "", err
	}
	if !changed {
		return "", nil
	}
	return deviceID, nil
}

// revokeGrantRow is a conditional UPDATE guarded on revoked_at IS NULL, so
// concurrent revokes of the same grant settle on exactly one winner.
func (r *Users) revokeGrantRow(ctx context.Context, where predicate.DeviceGrant, reason string, now time.Time) (string, bool, error) {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.DeviceGrant, error) {
		return r.client.DeviceGrant.Query().Where(where).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return "", false, ErrDeviceGrantNotFound
	}
	if err != nil {
		return "", false, err
	}
	if row.RevokedAt != nil {
		return row.DeviceID, false, nil
	}

	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.DeviceGrant.Update().
			Where(devicegrant.IDEQ(row.ID), devicegrant.RevokedAtIsNil()).
			SetRevokedAt(now).SetRevokedReason(reason).Save(ctx)
	})
	if err != nil {
		return "", false, err
	}
	return row.DeviceID, n == 1, nil
}

func (r *Users) publishDeviceRevoked(deviceID string) {
	if deviceID == "" || r.nc == nil {
		return
	}
	body, err := codec.Marshal(map[string]any{"device_id": deviceID, "revoked_at": time.Now().UTC()})
	if err != nil {
		r.log.Warn("controls.revoked encode failed", zap.String("device_id", deviceID), zap.Error(err))
		return
	}
	if err := r.nc.Publish(controlsRevokedSubjectPrefix+deviceID, body); err != nil {
		r.log.Warn("controls.revoked publish failed", zap.String("device_id", deviceID), zap.Error(err))
	}
}
