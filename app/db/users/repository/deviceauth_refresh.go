// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/pkg/db"
)

// RefreshDeviceGrant rotates the refresh token for its family. A replay of
// the just-rotated token revokes the grant unless it lands inside the 10s
// grace window, which reports invalid_grant without revoking.
func (r *Users) RefreshDeviceGrant(ctx context.Context, refreshToken string) (DeviceTokens, error) {
	if r.deviceSigner == nil {
		return DeviceTokens{}, ErrDeviceSigningKeyUnavailable
	}
	raw := decodeRefreshToken(refreshToken)
	if raw == nil {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	hash := hashDeviceSecret(raw)

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.DeviceGrant, error) {
		return r.client.DeviceGrant.Query().
			Where(devicegrant.Or(devicegrant.RefreshHashEQ(hash), devicegrant.PrevRefreshHashEQ(hash))).
			First(ctx)
	})
	if ent.IsNotFound(err) {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	if err != nil {
		return DeviceTokens{}, err
	}
	if row.RevokedAt != nil {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}

	now := time.Now().UTC()
	if constantTimeEqual(row.RefreshHash, hash) {
		return r.rotateDeviceGrant(ctx, row, hash, now)
	}
	return DeviceTokens{}, r.settleRefreshReplay(ctx, row, now)
}

func (r *Users) rotateDeviceGrant(ctx context.Context, row *ent.DeviceGrant, currentHash []byte, now time.Time) (DeviceTokens, error) {
	if err := r.requireDeviceOwner(ctx, row.UserID); err != nil {
		return DeviceTokens{}, err
	}
	if deviceGrantExpired(row, now) {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}

	newRaw, newHash, err := randomDeviceSecret()
	if err != nil {
		return DeviceTokens{}, err
	}
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.DeviceGrant.Update().
			Where(devicegrant.IDEQ(row.ID), devicegrant.RefreshHashEQ(currentHash), devicegrant.RevokedAtIsNil()).
			SetRefreshHash(newHash).SetPrevRefreshHash(currentHash).SetPrevRotatedAt(now).
			SetRefreshSlidingExpiresAt(now.Add(deviceRefreshSlidingTTL)).SetLastUsedAt(now).
			Save(ctx)
	})
	if err != nil {
		return DeviceTokens{}, err
	}
	if n != 1 {
		return DeviceTokens{}, r.settleLostRotation(ctx, row.ID, now)
	}
	return r.deviceTokensFor(row, newRaw, now)
}

func deviceGrantExpired(row *ent.DeviceGrant, now time.Time) bool {
	return now.After(row.RefreshAbsoluteExpiresAt) || now.After(row.RefreshSlidingExpiresAt)
}

func (r *Users) settleLostRotation(ctx context.Context, grantRowID int, now time.Time) error {
	fresh, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.DeviceGrant, error) {
		return r.client.DeviceGrant.Get(ctx, grantRowID)
	})
	if err != nil {
		return err
	}
	return r.settleRefreshReplay(ctx, fresh, now)
}

func (r *Users) deviceTokensFor(row *ent.DeviceGrant, refreshRaw []byte, now time.Time) (DeviceTokens, error) {
	access, err := r.signDeviceAccessToken(row.UserID, row.DeviceID, row.GrantID, now)
	if err != nil {
		return DeviceTokens{}, err
	}
	return DeviceTokens{
		AccessToken:  access,
		RefreshToken: deviceRefreshTokenPrefix + base64.RawURLEncoding.EncodeToString(refreshRaw),
		ExpiresIn:    int64(deviceAccessTokenTTL.Seconds()),
		GrantID:      row.GrantID,
		DeviceID:     row.DeviceID,
	}, nil
}

// settleRefreshReplay reports the rotated-token-reuse rule: a retry inside
// the grace window is quiet, anything older revokes the whole grant.
func (r *Users) settleRefreshReplay(ctx context.Context, row *ent.DeviceGrant, now time.Time) error {
	if withinRefreshGrace(row, now) {
		return ErrDeviceInvalidGrant
	}
	deviceID, changed, err := r.revokeGrantRow(ctx, devicegrant.IDEQ(row.ID), "refresh_reuse", now)
	if err != nil && err != ErrDeviceGrantNotFound {
		return err
	}
	if changed {
		r.publishDeviceRevoked(deviceID)
	}
	return ErrDeviceInvalidGrant
}

func withinRefreshGrace(row *ent.DeviceGrant, now time.Time) bool {
	return row.PrevRotatedAt != nil && now.Sub(*row.PrevRotatedAt) <= deviceRefreshReuseGrace
}

func decodeRefreshToken(token string) []byte {
	rest, ok := strings.CutPrefix(token, deviceRefreshTokenPrefix)
	if !ok {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return nil
	}
	return raw
}
