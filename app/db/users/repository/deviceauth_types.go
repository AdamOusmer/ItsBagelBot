// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/app/db/users/tokensign"

	"github.com/google/uuid"
)

const (
	deviceCodeTTL            = 60 * time.Second
	deviceAccessTokenTTL     = 15 * time.Minute
	deviceRefreshSlidingTTL  = 60 * 24 * time.Hour
	deviceRefreshAbsoluteTTL = 180 * 24 * time.Hour
	deviceRefreshReuseGrace  = 10 * time.Second
	deviceRefreshTokenPrefix = "bbr_"
	deviceCleanupCodeGrace   = 5 * time.Minute
	deviceCleanupGrantRetain = 7 * 24 * time.Hour
	deviceRandomBytes        = 32
)

// ErrDeviceInvalidGrant covers every exchange/refresh failure that an OAuth
// client must see as invalid_grant: unknown, expired, replayed or revoked.
var ErrDeviceInvalidGrant = errors.New("device authorization: invalid grant")

// ErrDeviceSigningKeyUnavailable means the Ed25519 signing key is not
// configured; the feature fails closed while the rest of Users keeps running.
var ErrDeviceSigningKeyUnavailable = errors.New("device authorization: signing key unavailable")

var ErrDeviceGrantNotFound = errors.New("device authorization: grant not found")

// errDeviceCodeRace never escapes the repository; it marks a lost race on an
// optimistic code-consume update so the caller re-reads and reports a replay.
var errDeviceCodeRace = errors.New("device authorization: code race")

// DeviceTokens is returned to RPC callers after a successful exchange or refresh.
type DeviceTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	GrantID      string
	DeviceID     string
}

func (r *Users) SetDeviceAuthSigner(signer *tokensign.Signer) {
	r.deviceSigner = signer
}

func randomDeviceSecret() (raw []byte, hash []byte, err error) {
	raw = make([]byte, deviceRandomBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	return raw, sum[:], nil
}

func hashDeviceSecret(raw []byte) []byte {
	sum := sha256.Sum256(raw)
	return sum[:]
}

func newDeviceUUID() string { return uuid.New().String() }

func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func verifyPKCE(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return constantTimeEqual([]byte(computed), []byte(challenge))
}

func (r *Users) signDeviceAccessToken(userID uint64, deviceID, grantID string, now time.Time) (string, error) {
	payload := tokensign.Payload{
		Iss:      tokensign.Issuer,
		Aud:      tokensign.Audience,
		Sub:      strconv.FormatUint(userID, 10),
		DeviceID: deviceID,
		GrantID:  grantID,
		Iat:      now.Unix(),
		Exp:      now.Add(deviceAccessTokenTTL).Unix(),
		Jti:      newDeviceUUID(),
	}
	return r.deviceSigner.Sign(payload)
}
