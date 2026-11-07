// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/authorizationcode"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/pkg/db"
)

// DeviceCodeRequest is what the consent page asks Users to bind a code to.
type DeviceCodeRequest struct {
	Challenge   string
	RedirectURI string
	DeviceName  string
	Platform    string
}

// CreateDeviceAuthorizationCode stores a single-use code hash; the raw code
// is returned once and never persisted.
func (r *Users) CreateDeviceAuthorizationCode(ctx context.Context, userID uint64, req DeviceCodeRequest) (string, error) {
	owner, err := r.FindUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if owner.Banned {
		return "", ErrDeviceInvalidGrant
	}

	raw, hash, err := randomDeviceSecret()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.AuthorizationCode.Create().
			SetCodeHash(hash).
			SetUserID(userID).
			SetCodeChallenge(req.Challenge).
			SetRedirectURI(req.RedirectURI).
			SetDeviceName(req.DeviceName).
			SetPlatform(authorizationcode.Platform(req.Platform)).
			SetCreatedAt(now).
			SetExpiresAt(now.Add(deviceCodeTTL)).
			Exec(ctx)
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ExchangeDeviceCode redeems a single-use code for a fresh device grant and
// token pair. It never consumes the code on a verifier or redirect mismatch,
// so a mistyped retry can still succeed before the code expires.
func (r *Users) ExchangeDeviceCode(ctx context.Context, code, verifier, redirectURI string) (DeviceTokens, error) {
	if r.deviceSigner == nil {
		return DeviceTokens{}, ErrDeviceSigningKeyUnavailable
	}
	hash := hashDeviceSecret(decodeDeviceSecret(code))

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.AuthorizationCode, error) {
		return r.client.AuthorizationCode.Query().Where(authorizationcode.CodeHashEQ(hash)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	if err != nil {
		return DeviceTokens{}, err
	}

	if outcome := r.checkConsumedCode(ctx, row); outcome != nil {
		return DeviceTokens{}, outcome
	}
	if !codeStillUsable(row, redirectURI, verifier, time.Now().UTC()) {
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}

	return r.redeemCode(ctx, row)
}

// checkConsumedCode reports the replay error and revokes any grant the code
// already produced; it returns nil when the code is still open to redeem.
func (r *Users) checkConsumedCode(ctx context.Context, row *ent.AuthorizationCode) error {
	if row.ConsumedAt == nil {
		return nil
	}
	r.revokeProducedGrant(ctx, row.ProducedGrantID)
	return ErrDeviceInvalidGrant
}

func codeStillUsable(row *ent.AuthorizationCode, redirectURI, verifier string, now time.Time) bool {
	if now.After(row.ExpiresAt) {
		return false
	}
	if row.RedirectURI != redirectURI {
		return false
	}
	return verifyPKCE(verifier, row.CodeChallenge)
}

func (r *Users) revokeProducedGrant(ctx context.Context, grantID *string) {
	if grantID == nil || *grantID == "" {
		return
	}
	deviceID, err := r.revokeGrantByGrantID(ctx, *grantID, "code_reuse")
	if err != nil || deviceID == "" {
		return
	}
	r.publishDeviceRevoked(deviceID)
}

// redeemCode performs the atomic create-grant-then-consume-code step. A lost
// race rolls the candidate grant back and is reported as the same replay.
func (r *Users) redeemCode(ctx context.Context, row *ent.AuthorizationCode) (DeviceTokens, error) {
	now := time.Now().UTC()
	refreshRaw, refreshHash, err := randomDeviceSecret()
	if err != nil {
		return DeviceTokens{}, err
	}

	grant := &ent.DeviceGrant{
		GrantID: newDeviceUUID(), DeviceID: newDeviceUUID(), UserID: row.UserID,
		DeviceName: row.DeviceName, Platform: devicegrant.Platform(row.Platform), RefreshHash: refreshHash,
		CreatedAt: now,
	}
	var tokens DeviceTokens
	err = db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
		if err := mintGrantInTx(ctx, tx, row.ID, grant); err != nil {
			return err
		}
		var err error
		tokens, err = r.deviceTokensFor(grant, refreshRaw, now)
		return err
	})

	if errors.Is(err, errDeviceCodeRace) {
		r.revokeProducedGrant(ctx, refetchProducedGrantID(ctx, r, row.ID))
		return DeviceTokens{}, ErrDeviceInvalidGrant
	}
	if err != nil {
		return DeviceTokens{}, err
	}
	return tokens, nil
}

func mintGrantInTx(ctx context.Context, tx *ent.Tx, codeRowID int, grant *ent.DeviceGrant) error {
	if err := requireDeviceOwnerInTx(ctx, tx, grant.UserID); err != nil {
		return err
	}
	now := grant.CreatedAt
	_, err := tx.DeviceGrant.Create().
		SetGrantID(grant.GrantID).
		SetDeviceID(grant.DeviceID).
		SetUserID(grant.UserID).
		SetDeviceName(grant.DeviceName).
		SetPlatform(grant.Platform).
		SetRefreshHash(grant.RefreshHash).
		SetRefreshSlidingExpiresAt(now.Add(deviceRefreshSlidingTTL)).
		SetRefreshAbsoluteExpiresAt(now.Add(deviceRefreshAbsoluteTTL)).
		SetCreatedAt(now).
		Save(ctx)
	if err != nil {
		return err
	}

	n, err := tx.AuthorizationCode.Update().
		Where(authorizationcode.IDEQ(codeRowID), authorizationcode.ConsumedAtIsNil(), authorizationcode.ExpiresAtGT(now)).
		SetConsumedAt(now).
		SetProducedGrantID(grant.GrantID).
		Save(ctx)
	if err != nil {
		return err
	}
	if n != 1 {
		return errDeviceCodeRace
	}
	return nil
}

func refetchProducedGrantID(ctx context.Context, r *Users, codeRowID int) *string {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.AuthorizationCode, error) {
		return r.client.AuthorizationCode.Get(ctx, codeRowID)
	})
	if err != nil {
		return nil
	}
	return row.ProducedGrantID
}

func decodeDeviceSecret(raw string) []byte {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	return b
}
